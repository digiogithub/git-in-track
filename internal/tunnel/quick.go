package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/client"
	cfdconfig "github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/ingress/origins"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// defaultAPIEndpoint is the anonymous tunnel broker. A POST to <endpoint>/tunnel
// with an empty body returns credentials and a *.trycloudflare.com hostname; no
// Cloudflare account and no authentication are involved.
const defaultAPIEndpoint = "https://api.trycloudflare.com"

// provisionTimeout bounds the single HTTP call that creates the tunnel.
const provisionTimeout = 15 * time.Second

// quickTunnelResponse is the trycloudflare.com broker's reply.
type quickTunnelResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Hostname   string `json:"hostname"`
		AccountTag string `json:"account_tag"`
		Secret     []byte `json:"secret"`
	} `json:"result"`
	Errors []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// provision asks the broker at endpoint for an anonymous tunnel and returns its
// credentials together with the public hostname (no scheme).
func provision(ctx context.Context, endpoint, userAgent string) (connection.Credentials, string, error) {
	var zero connection.Credentials

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/tunnel", bytes.NewReader(nil))
	if err != nil {
		return zero, "", fmt.Errorf("build quick tunnel request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	httpClient := &http.Client{Timeout: provisionTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return zero, "", fmt.Errorf("request quick tunnel: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do with a close error on a read body

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, "", fmt.Errorf("read quick tunnel response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return zero, "", fmt.Errorf("provision quick tunnel: status %d: %s", resp.StatusCode, truncate(body, 256))
	}

	var data quickTunnelResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return zero, "", fmt.Errorf("decode quick tunnel response %s: %w", truncate(body, 256), err)
	}
	if len(data.Errors) > 0 {
		first := data.Errors[0]
		return zero, "", fmt.Errorf("provision quick tunnel: broker error %d: %s", first.Code, first.Message)
	}
	if !data.Success {
		return zero, "", errBrokerRefused
	}
	if data.Result.Hostname == "" {
		return zero, "", fmt.Errorf("provision quick tunnel: %w", errNoHostname)
	}

	id, err := uuid.Parse(data.Result.ID)
	if err != nil {
		return zero, "", fmt.Errorf("parse quick tunnel id %q: %w", data.Result.ID, err)
	}
	return connection.Credentials{
		AccountTag:   data.Result.AccountTag,
		TunnelSecret: data.Result.Secret,
		TunnelID:     id,
	}, data.Result.Hostname, nil
}

// truncate shortens a response body so a broker failure cannot flood the log.
func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// quickTunnel is a provisioned but not yet running tunnel.
type quickTunnel struct {
	config       *supervisor.TunnelConfig
	orchestrator *orchestration.Orchestrator
	graceC       chan struct{}
	hostname     string
}

// buildTunnel assembles everything supervisor.NewSupervisor needs for a quick
// tunnel. It is a deliberate reimplementation of what cloudflared's own
// `tunnel --url` command does, because nothing under cloudflared/cmd/ may be
// imported: that tree calls sentry.Init, installs process-global signal
// handlers, starts an auto-updater and writes pidfiles.
//
// origin is a plain host:port that is already listening. cloudflared dials it
// as an ordinary loopback HTTP client, so the origin sees a normal request —
// note that the Host header carries the PUBLIC hostname
// (<name>.trycloudflare.com) unmodified, not the local address.
func buildTunnel(
	ctx context.Context,
	creds connection.Credentials,
	hostname, origin, version string,
	observer *connection.Observer,
	log *zerolog.Logger,
) (*quickTunnel, error) {
	// A fresh registry per tunnel: cloudflared's collectors use MustRegister,
	// so reusing the default registerer panics on the second tunnel.
	registry := prometheus.NewRegistry()

	featureSelector, err := features.NewFeatureSelector(ctx, creds.AccountTag, nil, false, log)
	if err != nil {
		return nil, fmt.Errorf("select cloudflared features: %w", err)
	}
	clientConfig, err := client.NewConfig(version, runtime.GOOS+"_"+runtime.GOARCH, featureSelector)
	if err != nil {
		return nil, fmt.Errorf("build cloudflared client config: %w", err)
	}
	tags := []pogs.Tag{{Name: "ID", Value: clientConfig.ConnectorID.String()}}

	// Exactly one catch-all ingress rule pointing at the local origin.
	ingressRules, err := ingress.ParseIngress(&cfdconfig.Configuration{
		Ingress: []cfdconfig.UnvalidatedIngressRule{
			{Service: "http://" + origin},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("parse ingress for origin %q: %w", origin, err)
	}

	protocolSelector, err := connection.NewProtocolSelector("quic", log)
	if err != nil {
		return nil, fmt.Errorf("select edge protocol: %w", err)
	}

	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, protocol := range connection.ProtocolList {
		settings := protocol.TLSSettings()
		if settings == nil {
			return nil, fmt.Errorf("edge protocol %s: %w", protocol, errUnknownTLSSettings)
		}
		cfg, err := tlsconfig.CreateTunnelConfig("", settings.ServerName)
		if err != nil {
			return nil, fmt.Errorf("build edge TLS config for %s: %w", protocol, err)
		}
		if len(settings.NextProtos) > 0 {
			cfg.NextProtos = settings.NextProtos
		}
		edgeTLSConfigs[protocol] = cfg
	}

	warpRouting := ingress.NewWarpRoutingConfig(&cfdconfig.WarpRoutingConfig{})
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   ingress.NewDialer(warpRouting),
		TCPWriteTimeout: 0,
	}, log)
	dnsService := origins.NewDNSResolverService(origins.NewDNSDialer(), log, origins.NewMetrics(registry))
	originDialer.AddReservedService(dnsService, []netip.AddrPort{origins.VirtualDNSServiceAddr})

	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:    clientConfig,
		GracePeriod:     gracePeriod,
		CloseConnOnce:   &sync.Once{},
		EdgeIPVersion:   allregions.Auto,
		HAConnections:   1,
		Tags:            tags,
		Log:             log,
		LogTransport:    log,
		Observer:        observer,
		ReportedVersion: version,
		Retries:         5,
		RunFromTerminal: false,

		NamedTunnel: &connection.TunnelProperties{
			Credentials:    creds,
			QuickTunnelUrl: hostname,
		},
		ProtocolSelector:   protocolSelector,
		EdgeTLSConfigs:     edgeTLSConfigs,
		MaxEdgeAddrRetries: 8,
		// Quick tunnels carry no ICMP proxy.
		ICMPRouterServer:    nil,
		OriginDNSService:    dnsService,
		OriginDialerService: originDialer,

		RPCTimeout:                          5 * time.Second,
		WriteStreamTimeout:                  0,
		DisableQUICPathMTUDiscovery:         false,
		QUICConnectionLevelFlowControlLimit: 30 * (1 << 20),
		QUICStreamLevelFlowControlLimit:     6 * (1 << 20),
	}

	// The fourth argument is internalRules and MUST stay nil. A rule with an
	// empty Hostname matches everything, and Ingress.FindMatchingRule consults
	// the internal rules before the real ones, so passing
	// ingress.GetDefaultIngressRules(log) makes every request 503 while the
	// logs still look healthy.
	orchestrator, err := orchestration.NewOrchestrator(ctx, &orchestration.Config{
		Ingress:             &ingressRules,
		WarpRouting:         warpRouting,
		OriginDialerService: originDialer,
		ConfigurationFlags:  map[string]string{"protocol": "quic", "ha-connections": "1"},
	}, tags, nil, log)
	if err != nil {
		return nil, fmt.Errorf("build tunnel orchestrator: %w", err)
	}

	return &quickTunnel{
		config:       tunnelConfig,
		orchestrator: orchestrator,
		graceC:       make(chan struct{}),
		hostname:     hostname,
	}, nil
}

// run blocks until ctx is cancelled or the tunnel dies unrecoverably.
func (t *quickTunnel) run(ctx context.Context) error {
	supervisorInstance, err := newSupervisor(t.config, t.orchestrator, t.graceC)
	if err != nil {
		return fmt.Errorf("build tunnel supervisor: %w", err)
	}
	if err := supervisorInstance.Run(ctx, signal.New(make(chan struct{}))); err != nil {
		return fmt.Errorf("run tunnel supervisor: %w", err)
	}
	return nil
}

// registererMu guards the prometheus.DefaultRegisterer swap in newSupervisor.
var registererMu sync.Mutex

// newSupervisor calls supervisor.NewSupervisor with a throwaway default
// registerer.
//
// supervisor.NewSupervisor hardcodes v3.NewMetrics(prometheus.DefaultRegisterer)
// and that constructor uses MustRegister, so building a second tunnel in the
// same process panics with "duplicate metrics collector registration
// attempted". cloudflared exposes no injection point, so the package-level
// registerer is swapped for the few microseconds construction takes and put
// back immediately. Calling supervisor.NewSupervisor directly, rather than
// supervisor.StartTunnelDaemon, is what keeps that window this short.
func newSupervisor(
	config *supervisor.TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	graceC <-chan struct{},
) (*supervisor.Supervisor, error) {
	var (
		s   *supervisor.Supervisor
		err error
	)
	withFreshDefaultRegisterer(func() {
		s, err = supervisor.NewSupervisor(config, orchestrator, graceC)
	})
	if err != nil {
		return nil, fmt.Errorf("new supervisor: %w", err)
	}
	return s, nil
}

// withFreshDefaultRegisterer runs fn with prometheus.DefaultRegisterer replaced
// by an empty registry, restoring the previous value before it returns. It
// serializes callers so two tunnels started concurrently cannot interleave the
// swap.
func withFreshDefaultRegisterer(fn func()) {
	registererMu.Lock()
	defer registererMu.Unlock()

	previous := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	defer func() { prometheus.DefaultRegisterer = previous }()

	fn()
}

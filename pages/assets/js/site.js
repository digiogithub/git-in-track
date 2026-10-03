/* git-in-track — marketing site behaviour. Vanilla JS, no dependencies. */
(function () {
  'use strict';

  var doc = document.documentElement;
  doc.classList.add('js');
  var reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var $ = function (sel, root) { return (root || document).querySelector(sel); };
  var $$ = function (sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); };

  function store(key, value) {
    try {
      if (value === undefined) return localStorage.getItem(key);
      localStorage.setItem(key, value);
    } catch (e) { /* storage may be unavailable */ }
    return null;
  }

  /* ── Theme toggle ─────────────────────────────────────── */
  var themeBtn = $('#theme-toggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var next = doc.dataset.theme === 'light' ? 'dark' : 'light';
      doc.dataset.theme = next;
      store('git-in-track-site:theme', next);
      toast('theme: ' + next + '  # persisted in localStorage, not in git (for once)');
    });
  }

  /* ── Toast ────────────────────────────────────────────── */
  var toastEl = $('#toast');
  var toastTimer;
  function toast(msg) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { toastEl.classList.remove('show'); }, 3200);
  }

  /* ── Scroll reveal ────────────────────────────────────── */
  var revealEls = $$('.reveal');
  if (reduceMotion || !('IntersectionObserver' in window)) {
    revealEls.forEach(function (el) { el.classList.add('in'); });
  } else {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -40px 0px' });
    revealEls.forEach(function (el) { io.observe(el); });
  }

  /* ── Card tilt + spotlight ────────────────────────────── */
  if (!reduceMotion && window.matchMedia('(hover: hover)').matches) {
    $$('.tilt').forEach(function (card) {
      card.addEventListener('pointermove', function (ev) {
        var r = card.getBoundingClientRect();
        var x = (ev.clientX - r.left) / r.width;
        var y = (ev.clientY - r.top) / r.height;
        card.style.transform = 'perspective(900px) rotateX(' + ((0.5 - y) * 6).toFixed(2) + 'deg) rotateY(' + ((x - 0.5) * 8).toFixed(2) + 'deg) translateY(-3px)';
        card.style.setProperty('--mx', (x * 100).toFixed(1) + '%');
        card.style.setProperty('--my', (y * 100).toFixed(1) + '%');
      });
      card.addEventListener('pointerleave', function () { card.style.transform = ''; });
    });
  }

  /* ── Copy buttons ─────────────────────────────────────── */
  $$('.copy').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.dataset.copyText;
      if (!text && btn.dataset.copy) { var src = document.getElementById(btn.dataset.copy); text = src ? src.textContent : ''; }
      if (!text) return;
      var done = function () { btn.textContent = 'copied'; setTimeout(function () { btn.textContent = 'copy'; }, 1500); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, function () { toast('clipboard unavailable here'); });
      } else { toast('clipboard unavailable here'); }
    });
  });

  /* ── Install tabs ─────────────────────────────────────── */
  var tabs = $$('.tabs [role="tab"]');
  function selectTab(tab) {
    tabs.forEach(function (t) {
      var on = t === tab;
      t.setAttribute('aria-selected', on ? 'true' : 'false');
      t.tabIndex = on ? 0 : -1;
      var panel = document.getElementById(t.getAttribute('aria-controls'));
      if (panel) panel.hidden = !on;
    });
  }
  tabs.forEach(function (tab, i) {
    tab.addEventListener('click', function () { selectTab(tab); });
    tab.addEventListener('keydown', function (ev) {
      var d = ev.key === 'ArrowRight' ? 1 : ev.key === 'ArrowLeft' ? -1 : 0;
      if (!d) return;
      var next = tabs[(i + d + tabs.length) % tabs.length];
      selectTab(next); next.focus(); ev.preventDefault();
    });
  });

  /* ── Counters ─────────────────────────────────────────── */
  if (!reduceMotion && 'IntersectionObserver' in window) {
    var cio = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) return;
        cio.unobserve(e.target);
        var el = e.target, target = +el.dataset.count, start = performance.now();
        if (!target) return;
        (function step(now) {
          var p = Math.min(1, (now - start) / 1200);
          el.textContent = Math.round(target * (1 - Math.pow(1 - p, 3)));
          if (p < 1) requestAnimationFrame(step);
        })(start);
      });
    }, { threshold: 0.6 });
    $$('[data-count]').forEach(function (el) { cio.observe(el); });
  }

  /* ── Typewriter terminal engine ───────────────────────── */
  // A script is a list of lines: {t:'cmd'|'out', s:'text', c:'css class', d: delay ms}
  function esc(s) { return s.replace(/[&<>]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]; }); }

  function Terminal(el, script, opts) {
    this.el = el; this.script = script; this.opts = opts || {}; this.run = 0;
  }
  Terminal.prototype.renderAll = function () {
    var html = this.script.map(function (l) { return lineHtml(l, l.s); }).join('\n');
    this.el.innerHTML = html;
    if (this.opts.onStep) this.opts.onStep(Infinity);
  };
  function lineHtml(l, text) {
    if (l.t === 'cmd') return '<span class="p">$ </span>' + esc(text);
    if (l.t === 'raw') return text;
    return (l.c ? '<span class="' + l.c + '">' + esc(text) + '</span>' : esc(text));
  }
  Terminal.prototype.play = function () {
    var self = this, runId = ++this.run, i = 0, done = '';
    if (reduceMotion) { this.renderAll(); return; }
    this.el.innerHTML = '<span class="cursor"></span>';
    function next() {
      if (runId !== self.run) return;
      if (i >= self.script.length) {
        self.el.innerHTML = done + '<span class="cursor"></span>';
        if (self.opts.loop) setTimeout(function () { if (runId === self.run) self.play(); }, self.opts.loop);
        return;
      }
      var l = self.script[i++];
      if (self.opts.onStep && l.step) self.opts.onStep(l.step);
      if (l.t === 'cmd') {
        var k = 0;
        (function type() {
          if (runId !== self.run) return;
          k++;
          self.el.innerHTML = done + lineHtml(l, l.s.slice(0, k)) + '<span class="cursor"></span>';
          if (k < l.s.length) setTimeout(type, 22 + Math.random() * 40);
          else { done += lineHtml(l, l.s) + '\n'; setTimeout(next, l.d || 380); }
        })();
      } else {
        done += lineHtml(l, l.s) + '\n';
        self.el.innerHTML = done + '<span class="cursor"></span>';
        setTimeout(next, l.d || 70);
      }
    }
    setTimeout(next, 400);
  };

  var heroScript = [
    { t: 'cmd', s: 'gintrack add ./acme' },
    { t: 'out', s: 'registered acme  docs: docs/  key: ACME  items: 214', c: 'ok', d: 500 },
    { t: 'cmd', s: 'head -4 docs/.pmngr/stories/ACME-US-0042-*.md' },
    { t: 'out', s: '---', c: 'acc' },
    { t: 'raw', s: '<span class="key">id</span>: ACME-US-0042' },
    { t: 'raw', s: '<span class="key">title</span>: Login with SSO' },
    { t: 'raw', s: '<span class="key">status</span>: <span class="warn">in_progress</span>', d: 500 },
    { t: 'cmd', s: 'gintrack serve' },
    { t: 'out', s: 'serving  http://127.0.0.1:7317   watching 3 repos', c: 'ok' },
    { t: 'out', s: 'mcp      POST /mcp (with --mcp-http)', c: 'dim', d: 700 },
    { t: 'cmd', s: 'git log --oneline -3 -- docs/.pmngr' },
    { t: 'raw', s: '<span class="warn">a1f3c9e</span> feat(core): claim ACME-US-0042' },
    { t: 'raw', s: '<span class="warn">7b20d14</span> docs(backlog): add SSO acceptance criteria' },
    { t: 'raw', s: '<span class="warn">e04c2aa</span> docs(backlog): open ACME-EP-0003 auth', d: 2600 }
  ];

  var loopScript = [
    { t: 'out', s: '# session: claude-code · 32 tools (writes on)', c: 'dim', d: 300 },
    { t: 'raw', s: '<span class="acc">→ list_items</span> type:story status:todo', step: 1 },
    { t: 'raw', s: '    label:agent-ok fields:[id,title,priority]' },
    { t: 'raw', s: '<span class="ok">← 2 rows</span>  ACME-US-0042  Login with SSO     high', d: 120 },
    { t: 'raw', s: '          ACME-US-0047  Audit log export  medium' },
    { t: 'raw', s: '          <span class="dim">nextCursor: "c2"</span>', d: 600 },
    { t: 'raw', s: '<span class="acc">→ get_item</span> ACME-US-0042 include:[body,comments]', step: 2 },
    { t: 'raw', s: '<span class="ok">←</span> rev sha256:9c1e4b07a2d3f816', d: 120 },
    { t: 'raw', s: '  blocked_by ACME-US-0038 <span class="ok">done</span>', d: 600 },
    { t: 'raw', s: '<span class="acc">→ update_item</span> status:in_progress', step: 3 },
    { t: 'raw', s: '    assignees:[claude-code] rev:sha256:9c1e…' },
    { t: 'raw', s: '<span class="ok">← ok</span> rev sha256:41aa7f0c9e2b5d13', d: 120 },
    { t: 'out', s: '  # a racing agent now gets stale_revision', c: 'dim', d: 700 },
    { t: 'raw', s: '<span class="acc">→ spec_context</span> story:ACME-US-0042 format:text', step: 4 },
    { t: 'raw', s: '<span class="ok">←</span> ACME-SP-0003.R2 untested "Sessions survive"', d: 500 },
    { t: 'out', s: '  … implement · // Implements: ACME-SP-0003.R2', c: 'dim', d: 400 },
    { t: 'out', s: '  … go test · spec ingest · spec_impact: clean', c: 'dim', d: 700 },
    { t: 'raw', s: '<span class="acc">→ add_comment</span> "branch feat/core-sso, plan: …"', step: 5 },
    { t: 'raw', s: '<span class="ok">← created</span> comments/ACME-US-0042/…-claude-code.md', d: 500 },
    { t: 'raw', s: '<span class="acc">→ update_item</span> status:in_review rev:sha256:41aa…', step: 6 },
    { t: 'raw', s: '<span class="ok">← ok</span>  over to a human reviewer ✓', d: 3000 }
  ];

  var heroEl = $('#hero-term');
  if (heroEl) new Terminal(heroEl, heroScript, { loop: 2500 }).play();

  var loopEl = $('#loop-term');
  var steps = $$('.loop-steps li');
  function markStep(n) {
    steps.forEach(function (li, idx) { li.classList.toggle('on', idx < n); });
  }
  if (loopEl) {
    var loopTerm = new Terminal(loopEl, loopScript, { onStep: markStep, loop: 0 });
    var started = false;
    var start = function () { if (!started) { started = true; markStep(0); loopTerm.play(); } };
    if ('IntersectionObserver' in window && !reduceMotion) {
      var lio = new IntersectionObserver(function (es) { if (es[0].isIntersecting) { start(); lio.disconnect(); } }, { threshold: 0.3 });
      lio.observe(loopEl);
    } else { loopTerm.renderAll(); }
    var replay = $('#replay-loop');
    if (replay) replay.addEventListener('click', function () { markStep(0); loopTerm.play(); });
  }

  /* ── Semantic search query cycling ────────────────────── */
  var semQ = $('#sem-q');
  if (semQ && !reduceMotion) {
    var queries = ['how are merge conflicts resolved', 'who decided the ID format?', 'what stops two agents taking one story'];
    var qi = 0;
    setInterval(function () { qi = (qi + 1) % queries.length; semQ.textContent = queries[qi]; }, 4200);
  }

  /* ── Screenshot gallery: lightbox, and the illustration as a fallback ── */
  var gallery = $('#gallery');
  var mock = $('#mock-tour');
  if (gallery) {
    var shots = $$('.shot', gallery);
    var showMockIfEmpty = function () {
      if (!$$('.shot', gallery).length) { gallery.remove(); if (mock) mock.hidden = false; }
    };
    shots.forEach(function (fig) {
      var img = fig.querySelector('img');
      if (!img) { fig.remove(); return; }
      var drop = function () { fig.remove(); showMockIfEmpty(); };
      if (img.complete && img.naturalWidth === 0 && img.currentSrc) drop();
      else img.addEventListener('error', drop);
      fig.tabIndex = 0;
      fig.setAttribute('role', 'button');
      fig.setAttribute('aria-label', 'Enlarge: ' + img.alt);
      var open = function () {
        var lb = document.createElement('div');
        lb.className = 'lightbox';
        lb.setAttribute('role', 'dialog');
        lb.setAttribute('aria-label', img.alt);
        lb.innerHTML = '<div><img alt=""><p></p></div>';
        lb.querySelector('img').src = img.currentSrc || img.src;
        lb.querySelector('img').alt = img.alt;
        lb.querySelector('p').textContent = img.alt + '  ·  esc to close';
        var close = function () { lb.remove(); document.removeEventListener('keydown', onKey); fig.focus(); };
        var onKey = function (ev) { if (ev.key === 'Escape') close(); };
        lb.addEventListener('click', close);
        document.addEventListener('keydown', onKey);
        document.body.appendChild(lb);
      };
      fig.addEventListener('click', open);
      fig.addEventListener('keydown', function (ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); open(); } });
    });
    showMockIfEmpty();
  }

  /* ── Background: drifting commit graph (or Markdown rain) ── */
  var canvas = $('#graph-bg');
  var mode = 'graph';
  if (canvas && canvas.getContext) {
    var ctx = canvas.getContext('2d');
    var W = 0, H = 0, dpr = Math.min(window.devicePixelRatio || 1, 2);
    var nodes = [], drops = [];
    var glyphs = '#*-_[]()`>|:~=+!0123456789abcdef---'.split('');

    var rgb = function (name) { return getComputedStyle(doc).getPropertyValue(name).trim() || '139, 108, 255'; };

    var resize = function () {
      W = window.innerWidth; H = window.innerHeight;
      canvas.width = W * dpr; canvas.height = H * dpr;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      seed();
    };
    var seed = function () {
      // Lanes of commits, like `git log --graph`, drifting slowly upward.
      var lanes = Math.max(4, Math.round(W / 140));
      nodes = [];
      var count = Math.round((W * H) / 26000);
      for (var i = 0; i < count; i++) {
        var lane = i % lanes;
        nodes.push({
          lane: lane,
          x: (lane + 0.5) * (W / lanes) + (Math.random() - 0.5) * 60,
          y: Math.random() * H,
          r: 1.6 + Math.random() * 2.4,
          v: 0.08 + Math.random() * 0.22,
          c: Math.random() < 0.35 ? 2 : 1,
          ph: Math.random() * Math.PI * 2
        });
      }
      drops = [];
      var cols = Math.ceil(W / 18);
      for (var j = 0; j < cols; j++) drops.push({ x: j * 18, y: Math.random() * -H, v: 2 + Math.random() * 4 });
    };

    var drawGraph = function (t) {
      ctx.clearRect(0, 0, W, H);
      var c1 = rgb('--graph-node'), c2 = rgb('--graph-node-2');
      var maxD = 150;
      for (var i = 0; i < nodes.length; i++) {
        var a = nodes[i];
        for (var j = i + 1; j < nodes.length; j++) {
          var b = nodes[j];
          var dx = a.x - b.x, dy = a.y - b.y;
          var d2 = dx * dx + dy * dy;
          if (d2 < maxD * maxD) {
            var alpha = (1 - Math.sqrt(d2) / maxD) * (a.lane === b.lane ? 0.32 : 0.12);
            ctx.strokeStyle = 'rgba(' + c1 + ',' + alpha.toFixed(3) + ')';
            ctx.lineWidth = a.lane === b.lane ? 1.2 : 0.7;
            ctx.beginPath();
            ctx.moveTo(a.x, a.y);
            if (a.lane === b.lane) ctx.lineTo(b.x, b.y);
            else ctx.bezierCurveTo(a.x, (a.y + b.y) / 2, b.x, (a.y + b.y) / 2, b.x, b.y);
            ctx.stroke();
          }
        }
      }
      for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        var pulse = 0.55 + 0.35 * Math.sin(t / 900 + n.ph);
        ctx.fillStyle = 'rgba(' + (n.c === 2 ? c2 : c1) + ',' + pulse.toFixed(3) + ')';
        ctx.beginPath(); ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2); ctx.fill();
        n.y -= n.v;
        n.x += Math.sin(t / 3000 + n.ph) * 0.05;
        if (n.y < -10) { n.y = H + 10; }
      }
    };

    var drawRain = function () {
      ctx.fillStyle = doc.dataset.theme === 'light' ? 'rgba(247,247,251,0.18)' : 'rgba(7,8,13,0.18)';
      ctx.fillRect(0, 0, W, H);
      ctx.font = '14px "JetBrains Mono", monospace';
      var c2 = rgb('--graph-node-2');
      for (var i = 0; i < drops.length; i++) {
        var d = drops[i];
        ctx.fillStyle = 'rgba(' + c2 + ',0.85)';
        ctx.fillText(glyphs[(Math.random() * glyphs.length) | 0], d.x, d.y);
        d.y += d.v * 4;
        if (d.y > H && Math.random() > 0.97) d.y = Math.random() * -100;
      }
    };

    var visible = true;
    document.addEventListener('visibilitychange', function () { visible = !document.hidden; });
    var frame = function (t) {
      if (visible) { if (mode === 'rain') drawRain(); else drawGraph(t); }
      requestAnimationFrame(frame);
    };

    window.addEventListener('resize', function () { clearTimeout(resize.t); resize.t = setTimeout(resize, 150); });
    resize();
    if (reduceMotion) drawGraph(0); else requestAnimationFrame(frame);

    /* ── Easter egg: Konami code → Markdown rain ────────── */
    var konami = ['ArrowUp', 'ArrowUp', 'ArrowDown', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'ArrowLeft', 'ArrowRight', 'b', 'a'];
    var pos = 0;
    document.addEventListener('keydown', function (ev) {
      var key = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
      pos = key === konami[pos] ? pos + 1 : (key === konami[0] ? 1 : 0);
      if (pos === konami.length) {
        pos = 0;
        mode = mode === 'rain' ? 'graph' : 'rain';
        document.body.classList.toggle('rain', mode === 'rain');
        if (reduceMotion) { if (mode === 'rain') drawRain(); else drawGraph(0); }
        toast(mode === 'rain'
          ? 'git checkout -b matrix  # status: wake_up, assignees: [neo]'
          : 'git switch main  # back to the commit graph');
      }
    });
  }

  /* ── Easter egg: type ":wq" anywhere ──────────────────── */
  var typed = '';
  document.addEventListener('keydown', function (ev) {
    if (ev.target && /input|textarea/i.test(ev.target.tagName)) return;
    if (ev.key.length !== 1) return;
    typed = (typed + ev.key).slice(-3);
    if (typed === ':wq') toast('"backlog.md" written. rev: sha256:' + Math.random().toString(16).slice(2, 18));
  });

  /* ── Console banner for the curious ───────────────────── */
  try {
    console.log('%c---\ntype: easter-egg\ntitle: You opened the console\nstatus: in_progress\n---', 'font-family:monospace;color:#22d3a6;font-size:13px');
    console.log('%cThe whole backlog of this project lives in docs/.pmngr/. Try: ↑ ↑ ↓ ↓ ← → ← → b a', 'font-family:monospace;color:#a78bfa');
  } catch (e) { /* no console */ }
})();

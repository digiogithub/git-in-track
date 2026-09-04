import { X } from 'lucide-react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';

import { cn } from '@/lib/cn';

export type ToastVariant = 'default' | 'destructive';

export type ToastInput = {
  title: string;
  description?: string;
  variant?: ToastVariant;
  /** Milliseconds before the toast dismisses itself; `0` keeps it until dismissed. */
  durationMs?: number;
};

type ToastRecord = ToastInput & { id: number };

type ToastApi = {
  toast: (input: ToastInput) => void;
  dismiss: (id: number) => void;
};

const noop: ToastApi = { toast: () => undefined, dismiss: () => undefined };

const ToastContext = createContext<ToastApi>(noop);

/** Imperative toast API. Outside a `ToastProvider` it is a no-op. */
export function useToast(): ToastApi {
  return useContext(ToastContext);
}

const DEFAULT_DURATION_MS = 6_000;

/**
 * Toast host. Notifications are announced politely and are also readable by
 * tests through their role, so a failed write is never silent.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([]);
  const nextId = useRef(1);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback(
    (input: ToastInput) => {
      const id = nextId.current;
      nextId.current += 1;
      setToasts((current) => [...current, { ...input, id }]);
      const duration = input.durationMs ?? DEFAULT_DURATION_MS;
      if (duration > 0) {
        const handle = setTimeout(() => {
          timers.current.delete(handle);
          dismiss(id);
        }, duration);
        timers.current.add(handle);
      }
    },
    [dismiss],
  );

  useEffect(() => {
    const handles = timers.current;
    return () => {
      for (const handle of handles) clearTimeout(handle);
      handles.clear();
    };
  }, []);

  const api = useMemo<ToastApi>(() => ({ toast, dismiss }), [toast, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ol
        aria-live="polite"
        aria-label="Notifications"
        className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2"
      >
        {toasts.map((item) => (
          <li
            key={item.id}
            className={cn(
              'pointer-events-auto rounded-md border bg-card p-3 pr-9 text-sm shadow-lg',
              item.variant === 'destructive'
                ? 'border-destructive/40 text-destructive'
                : 'border-border text-card-foreground',
            )}
          >
            <div className="relative">
              <p className="font-medium">{item.title}</p>
              {item.description ? (
                <p className="mt-0.5 text-xs text-muted-foreground">{item.description}</p>
              ) : null}
              <button
                type="button"
                aria-label="Dismiss notification"
                onClick={() => {
                  dismiss(item.id);
                }}
                className="absolute -right-6 -top-1 rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              >
                <X aria-hidden="true" className="h-3.5 w-3.5" />
              </button>
            </div>
          </li>
        ))}
      </ol>
    </ToastContext.Provider>
  );
}

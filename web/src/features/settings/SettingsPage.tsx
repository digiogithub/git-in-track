import { useAppStore } from '@/app/store';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

/** Settings shell. Workspace, appearance, sync, credentials and agents follow. */
export function SettingsPage() {
  const mode = useAppStore((state) => state.mode);
  const companionVersion = useAppStore((state) => state.companionVersion);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">Workspace, appearance, sync and agents.</p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Runtime</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm">
          <p>
            Mode: <strong>{mode}</strong>
          </p>
          <p className="text-muted-foreground">
            Companion version: {companionVersion ?? 'not detected'}
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

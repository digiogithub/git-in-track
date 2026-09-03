import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

/** Board index. Kanban and scrum boards arrive with the team repository (Phase 3). */
export function BoardList() {
  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Boards</h1>
        <p className="text-sm text-muted-foreground">
          Boards live in the team repository and pull cards from every configured project.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>No team repository mounted</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Mount a team repository to see its boards, sprints and retrospectives.
        </CardContent>
      </Card>
    </div>
  );
}

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import type { QueryLog } from "@/types"

type Props = {
  running: boolean
  status: "idle" | "running" | "complete" | "error"
  completedCount: number
  totalResolvers: number
  progress: number
  domainCount: number
  fallbackDomainCount: number
  resolverCount: number
  runId: string | null
  activeResolver: string | null
  queryLog: QueryLog[]
}

export function LivePanel({
  running,
  status,
  completedCount,
  totalResolvers,
  progress,
  domainCount,
  fallbackDomainCount,
  resolverCount,
  runId,
  activeResolver,
  queryLog,
}: Props) {
  return (
    <Card className="border-muted/70 bg-card/60 shadow-sm backdrop-blur">
      <CardHeader className="pb-3">
        <CardTitle>Live run</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-2">
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">
              {completedCount}/{totalResolvers || "?"} resolvers
            </span>
            <span className="font-semibold">{progress}%</span>
          </div>
          <Progress value={progress} />
        </div>
        <div className="flex flex-wrap gap-2 text-sm text-muted-foreground">
          <Badge variant="secondary">Domains: {domainCount || fallbackDomainCount}</Badge>
          <Badge variant="secondary">Resolvers: {resolverCount}</Badge>
          <Badge variant="secondary">Run: {runId ? runId.slice(-6) : "—"}</Badge>
          <Badge variant={status === "error" ? "destructive" : "outline"}>{status}</Badge>
        </div>
        {(running || activeResolver) ? (
          <div className="rounded-lg border bg-muted/30 p-3">
            <p className="text-xs uppercase tracking-tight text-muted-foreground">Currently querying</p>
            <p className="text-lg font-semibold">{activeResolver ?? "resolving…"}</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            No active resolver — start a benchmark to see live activity.
          </p>
        )}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <p className="text-sm font-medium">Event log</p>
            <Badge variant="outline">{queryLog.length} recent</Badge>
          </div>
          <ScrollArea className="h-[260px] rounded-md border bg-muted/30 p-3">
            <div className="space-y-2 text-sm">
              {queryLog.length === 0 && <p className="text-muted-foreground">Waiting for events…</p>}
              {queryLog.map((q, idx) => (
                <div
                  key={`${q.domain}-${q.ts}-${idx}`}
                  className={cn(
                    "flex items-center justify-between rounded border px-2 py-1",
                    q.error ? "border-destructive/50 bg-destructive/10" : "border-border bg-background/60",
                  )}
                >
                  <div className="flex flex-col">
                    <span className="font-medium">{q.server}</span>
                    <span className="text-xs text-muted-foreground">{q.domain}</span>
                  </div>
                  <div className="text-xs font-semibold">
                    {q.error ? <span className="text-destructive">{q.error}</span> : `${q.latency?.toFixed(1)} ms`}
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
        </div>
      </CardContent>
    </Card>
  )
}

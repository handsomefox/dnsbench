import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { formatMs, successRate } from "@/lib/benchmark-helpers"
import type { BenchmarkResult } from "@/types"
import { ResultStat } from "./ResultStat"

type Props = {
  results: BenchmarkResult[]
  totalResolvers: number
}

export function ResultsPanel({ results, totalResolvers }: Props) {
  const completedCount = results.length
  const errors = results.reduce((sum, r) => sum + r.stats.errors, 0)
  const best = results[0]

  return (
    <Card className="border-muted/70 bg-card/60 shadow-sm backdrop-blur">
      <CardHeader>
        <CardTitle>Results</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-2 gap-3 text-sm">
          <ResultStat label="Completed" value={`${completedCount}/${totalResolvers || "?"}`} />
          <ResultStat label="Best mean" value={formatMs(best?.stats?.mean)} />
          <ResultStat label="Highest success" value={`${(successRate(best?.stats) || 0).toFixed(1)}%`} />
          <ResultStat label="Errors" value={errors.toString()} />
        </div>
        <ScrollArea className="h-[360px] rounded-md border bg-muted/30">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-muted/60 text-left">
              <tr>
                <th className="px-3 py-2">Resolver</th>
                <th className="px-3 py-2">Mean</th>
                <th className="px-3 py-2">Success</th>
                <th className="px-3 py-2">Min/Max</th>
              </tr>
            </thead>
            <tbody>
              {results.length === 0 && (
                <tr>
                  <td className="px-3 py-3 text-muted-foreground" colSpan={4}>
                    No results yet.
                  </td>
                </tr>
              )}
              {results.map((r) => (
                <tr key={r.server.addr} className="border-t border-border/60">
                  <td className="px-3 py-2">
                    <div className="font-medium">{r.server.name}</div>
                    <div className="text-xs text-muted-foreground">{r.server.addr}</div>
                  </td>
                  <td className="px-3 py-2 font-semibold">{formatMs(r.stats.mean)}</td>
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-2">
                      <Progress value={successRate(r.stats)} className="h-2 w-24" />
                      <span className="text-xs font-semibold">{successRate(r.stats).toFixed(1)}%</span>
                    </div>
                  </td>
                  <td className="px-3 py-2 text-xs text-muted-foreground">
                    {formatMs(r.stats.min)} / {formatMs(r.stats.max)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollArea>
      </CardContent>
    </Card>
  )
}

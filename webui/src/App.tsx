import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { toast } from "@/components/ui/sonner"
import { parseDomains, parseResolvers, successRate } from "@/lib/benchmark-helpers"
import { ConfigPanel } from "@/components/dashboard/ConfigPanel"
import { LivePanel } from "@/components/dashboard/LivePanel"
import { ResultsPanel } from "@/components/dashboard/ResultsPanel"
import type {
  BenchmarkResult,
  DefaultsResponse,
  DNSServer,
  QueryLog,
  RunOptions,
  SSEMessage,
  Stats,
} from "./types"

const fallbackOptions: RunOptions = {
  repeats: 10,
  timeoutMs: 3000,
  concurrency: 4,
  warmup: 0,
  onlyMajor: false,
}

function App() {
  const [defaults, setDefaults] = useState<DefaultsResponse | null>(null)
  const [domainsText, setDomainsText] = useState("")
  const [customResolvers, setCustomResolvers] = useState("")
  const [options, setOptions] = useState<RunOptions>(fallbackOptions)
  const [runId, setRunId] = useState<string | null>(null)
  const runIdRef = useRef<string | null>(null)
  const [status, setStatus] = useState<"idle" | "running" | "complete" | "error">("idle")
  const [activeResolver, setActiveResolver] = useState<string | null>(null)
  const [domainCount, setDomainCount] = useState(0)
  const [totalResolvers, setTotalResolvers] = useState(0)
  const [liveResults, setLiveResults] = useState<Record<string, BenchmarkResult>>({})
  const [queryLog, setQueryLog] = useState<QueryLog[]>([])
  const [showResultsModal, setShowResultsModal] = useState(false)

  useEffect(() => {
    runIdRef.current = runId
  }, [runId])

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)")
    const apply = () => document.documentElement.classList.toggle("dark", mq.matches)
    apply()
    mq.addEventListener("change", apply)
    return () => mq.removeEventListener("change", apply)
  }, [])

  useEffect(() => {
    const loadDefaults = async () => {
      try {
        const res = await fetch("/api/defaults")
        if (!res.ok) throw new Error("Failed to load defaults")
        const data: DefaultsResponse = await res.json()
        setDefaults(data)
        setDomainsText(data.domains.join("\n"))
        setOptions(data.options)
      } catch (err) {
        console.error(err)
        toast.error("Could not load defaults; using fallbacks.")
        setOptions(fallbackOptions)
      }
    }
    loadDefaults()
  }, [])

  const resetAll = useCallback(() => {
    setRunId(null)
    runIdRef.current = null
    setStatus("idle")
    setActiveResolver(null)
    setDomainCount(0)
    setTotalResolvers(0)
    setLiveResults({})
    setQueryLog([])
    setShowResultsModal(false)
    if (defaults) {
      setDomainsText(defaults.domains.join("\n"))
      setOptions(defaults.options)
    } else {
      setDomainsText("")
      setOptions(fallbackOptions)
    }
  }, [defaults])

  const handleEvent = useCallback(
    (msg: SSEMessage) => {
      const detail = msg.detail as Record<string, unknown> | undefined
      switch (msg.type) {
        case "start": {
          setStatus("running")
          setActiveResolver(null)
          setLiveResults({})
          setQueryLog([])
          setDomainCount((detail?.domainCount as number) ?? 0)
          setTotalResolvers((detail?.totalResolvers as number) ?? 0)
          if (!runIdRef.current && msg.runId) {
            setRunId(msg.runId)
          }
          break
        }
        case "stop": {
          setStatus("idle")
          setActiveResolver(null)
          setRunId(null)
          toast("Benchmark stopped")
          break
        }
        case "resolver_start":
          setActiveResolver((detail?.server as DNSServer | undefined)?.name ?? null)
          break
        case "query": {
          if (detail) {
            const serverDetail = detail.server as DNSServer | undefined
            setQueryLog((prev) => {
              const next = [
                {
                  domain: (detail.domain as string) ?? "",
                  server:
                    serverDetail?.name ??
                    serverDetail?.addr ??
                    activeResolver ??
                    "resolver",
                  latency: typeof detail.latency === "number" ? (detail.latency as number) : undefined,
                  error: typeof detail.error === "string" ? (detail.error as string) : undefined,
                  ts: Date.now(),
                },
                ...prev,
              ].slice(0, 80)
              return next
            })
            if (serverDetail) {
              const latency = typeof detail.latency === "number" ? (detail.latency as number) : undefined
              const hadError = Boolean(detail.error)
              setLiveResults((prev) => {
                const existing = prev[serverDetail.addr] ?? { server: serverDetail, stats: emptyStats() }
                const updatedStats = bumpStats(existing.stats, latency, hadError)
                return {
                  ...prev,
                  [serverDetail.addr]: { server: serverDetail, stats: updatedStats },
                }
              })
            }
          }
          break
        }
        case "resolver_done": {
          const server: DNSServer | undefined = detail?.server as DNSServer | undefined
          const stats = detail?.stats as Stats | undefined
          if (server && stats) {
            setLiveResults((prev) => ({
              ...prev,
              [server.addr]: { server, stats },
            }))
          }
          break
        }
        case "complete": {
          setActiveResolver(null)
          setStatus(detail?.error ? "error" : "complete")
          if (detail?.results) {
            const next: Record<string, BenchmarkResult> = {}
            ;(detail.results as BenchmarkResult[]).forEach((r) => {
              next[r.server.addr] = r
            })
            setLiveResults(next)
          }
          if (detail?.error && typeof detail.error === "string") {
            toast.error(detail.error)
          } else {
            toast.success("Benchmark complete")
            setShowResultsModal(true)
          }
          break
        }
        case "reset": {
          resetAll()
          break
        }
        default:
          break
      }
    },
    [activeResolver, resetAll]
  )

  useEffect(() => {
    const es = new EventSource("/api/events")
    es.onmessage = (event) => {
      try {
        const msg: SSEMessage = JSON.parse(event.data)
        if (!msg || !msg.type || msg.type === "ping" || msg.type === "ready") return
        if (msg.runId && runIdRef.current && msg.runId !== runIdRef.current) return
        handleEvent(msg)
      } catch (err) {
        console.error("Failed to parse SSE event", err)
      }
    }
    es.onerror = () => {
      toast.error("Lost connection to server events. Refresh if issues persist.")
    }
    return () => es.close()
  }, [handleEvent])

  const parsedResolvers = useMemo(() => parseResolvers(customResolvers), [customResolvers])
  const preparedDomains = useMemo(() => parseDomains(domainsText), [domainsText])

  const resolverList = parsedResolvers.length
    ? parsedResolvers
    : options.onlyMajor
      ? defaults?.majorResolvers ?? []
      : defaults?.resolvers ?? []

const resultsTable = useMemo(() => {
  const values = Object.values(liveResults)
  return values.sort((a, b) => successRate(b.stats) - successRate(a.stats))
}, [liveResults])

  const startRun = async () => {
    if (!preparedDomains.length) {
      toast.error("Add at least one domain.")
      return
    }
    try {
      const payload = {
        domains: preparedDomains,
        resolvers: parsedResolvers,
        options,
      }
      const res = await fetch("/api/run", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      })
      if (!res.ok) throw new Error(await res.text())
      const data = await res.json()
      setRunId(data.runId)
      setStatus("running")
      setLiveResults({})
      setQueryLog([])
      toast("Benchmark started")
      setShowResultsModal(false)
    } catch (err: unknown) {
      console.error(err)
      toast.error(err instanceof Error ? err.message : "Failed to start benchmark")
    }
  }

  const stopRun = async () => {
    if (!running) return
    try {
      const res = await fetch("/api/stop", { method: "POST" })
      if (!res.ok) throw new Error(await res.text())
      setStatus("idle")
      setActiveResolver(null)
      setRunId(null)
    } catch (err: unknown) {
      console.error(err)
      toast.error(err instanceof Error ? err.message : "Failed to stop benchmark")
    }
  }

  const resetFromServer = async () => {
    try {
      const res = await fetch("/api/reset", { method: "POST" })
      if (!res.ok) throw new Error(await res.text())
      resetAll()
    } catch (err: unknown) {
      console.error(err)
      toast.error(err instanceof Error ? err.message : "Failed to reset")
    }
  }

  const running = status === "running"
  const completedCount = resultsTable.length
  const progress = totalResolvers ? Math.round((completedCount / totalResolvers) * 100) : 0

  return (
    <div className="min-h-screen bg-gradient-to-b from-primary/10 via-background to-secondary/10 text-foreground">
      <div className="mx-auto flex max-w-screen-2xl flex-col gap-8 px-6 py-8 lg:px-10">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p className="text-sm uppercase tracking-tight text-muted-foreground">dnsbench</p>
            <h1 className="text-3xl font-semibold text-primary">Dashboard</h1>
            <p className="text-sm text-muted-foreground">
              Configure benchmarks on the left, watch live progress in the center, inspect results on the right.
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Button variant="outline" disabled>
              {status === "running" ? "Running" : "Idle"}
            </Button>
            <Button
              variant="secondary"
              onClick={() => setShowResultsModal(true)}
              disabled={resultsTable.length === 0}
            >
              View results
            </Button>
            <Button variant="secondary" onClick={resetFromServer}>
              Reset
            </Button>
            <Button variant="destructive" onClick={stopRun} disabled={!running}>
              Stop
            </Button>
            <Button onClick={startRun} disabled={running}>
              {running ? "Running…" : "Start benchmark"}
            </Button>
          </div>
        </header>

        <div className="grid gap-6 lg:grid-cols-3 xl:grid-cols-[1.15fr_1fr_1fr]">
          <ConfigPanel
            domainsText={domainsText}
            onDomainsChange={setDomainsText}
            preparedDomainsCount={preparedDomains.length}
            options={options}
            onOptionsChange={setOptions}
            customResolvers={customResolvers}
            onCustomResolversChange={setCustomResolvers}
            parsedResolversCount={parsedResolvers.length}
            defaults={defaults}
          />

          <LivePanel
            running={running}
            status={status}
            completedCount={completedCount}
            totalResolvers={totalResolvers}
            progress={progress}
            domainCount={domainCount}
            fallbackDomainCount={preparedDomains.length}
            resolverCount={resolverList.length}
            runId={runId}
            activeResolver={activeResolver}
            queryLog={queryLog}
          />

          <ResultsPanel results={resultsTable} totalResolvers={totalResolvers} />
        </div>
      </div>
      {showResultsModal && (
        <ResultsModal
          results={resultsTable}
          totalResolvers={totalResolvers}
          onClose={() => setShowResultsModal(false)}
        />
      )}
    </div>
  )
}

export default App

function emptyStats(): Stats {
  return { min: 0, max: 0, mean: 0, count: 0, errors: 0, total: 0 }
}

function bumpStats(prev: Stats, latency?: number, hadError?: boolean): Stats {
  const next: Stats = { ...prev }
  next.total = (next.total ?? 0) + 1
  if (hadError) {
    next.errors = (next.errors ?? 0) + 1
    return next
  }
  if (latency === undefined || Number.isNaN(latency)) return next
  const count = (next.count ?? 0) + 1
  const mean = next.mean ?? 0
  const delta = latency - mean
  const newMean = mean + delta / count
  next.count = count
  next.mean = newMean
  next.min = next.count === 1 ? latency : Math.min(next.min, latency)
  next.max = next.count === 1 ? latency : Math.max(next.max, latency)
  return next
}

type ResultsModalProps = {
  results: BenchmarkResult[]
  totalResolvers: number
  onClose: () => void
}

function ResultsModal({ results, totalResolvers, onClose }: ResultsModalProps) {
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc")
  const sorted = useMemo(
    () =>
      [...results].sort((a, b) =>
        sortDir === "asc" ? a.stats.mean - b.stats.mean : b.stats.mean - a.stats.mean,
      ),
    [results, sortDir],
  )
  const top = sorted.slice(0, 12)
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-background/80 backdrop-blur-sm px-4">
      <div className="relative w-full max-w-5xl rounded-xl border bg-card p-6 shadow-2xl">
        <button
          onClick={onClose}
          className="absolute right-3 top-3 rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground hover:bg-muted/80"
        >
          Close
        </button>
        <div className="mb-4">
          <p className="text-xs uppercase tracking-tight text-muted-foreground">Summary</p>
          <h2 className="text-2xl font-semibold">Benchmark results</h2>
          <p className="text-sm text-muted-foreground">
            Completed resolvers: {results.length}/{totalResolvers || "?"}
          </p>
        </div>
        <div className="mb-3 flex items-center justify-between">
          <p className="text-sm text-muted-foreground">Sorted by mean latency</p>
          <div className="flex gap-2">
            <Button
              variant={sortDir === "asc" ? "secondary" : "outline"}
              size="sm"
              onClick={() => setSortDir("asc")}
            >
              Mean ↑
            </Button>
            <Button
              variant={sortDir === "desc" ? "secondary" : "outline"}
              size="sm"
              onClick={() => setSortDir("desc")}
            >
              Mean ↓
            </Button>
          </div>
        </div>
        <div className="overflow-hidden rounded-lg border">
          <table className="min-w-full divide-y divide-border text-sm">
            <thead className="bg-muted/60 text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left font-medium">Resolver</th>
                <th className="px-3 py-2 text-left font-medium">Success</th>
                <th className="px-3 py-2 text-left font-medium">Mean (ms)</th>
                <th className="px-3 py-2 text-left font-medium">Min / Max</th>
                <th className="px-3 py-2 text-left font-medium">Total</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border bg-card/60">
              {top.map((r) => (
                <tr key={r.server.addr} className="hover:bg-muted/40">
                  <td className="px-3 py-2">
                    <div className="font-semibold text-foreground">{r.server.name}</div>
                    <div className="text-xs text-muted-foreground">{r.server.addr}</div>
                  </td>
                  <td className="px-3 py-2 font-semibold">
                    {(successRate(r.stats) * 100).toFixed(1)}%
                  </td>
                  <td className="px-3 py-2">{r.stats.mean.toFixed(2)}</td>
                  <td className="px-3 py-2 text-xs">
                    {r.stats.min.toFixed(2)} / {r.stats.max.toFixed(2)}
                  </td>
                  <td className="px-3 py-2">{r.stats.total}</td>
                </tr>
              ))}
              {top.length === 0 && (
                <tr>
                  <td className="px-3 py-4 text-center text-muted-foreground" colSpan={5}>
                    No results yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

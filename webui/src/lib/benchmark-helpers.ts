import type { DNSServer, Stats } from "@/types"

export function parseDomains(input: string) {
  return input
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
}

export function parseResolvers(input: string): DNSServer[] {
  return input
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [name, addr] = line.split(";").map((s) => s.trim())
      return { name: name || addr, addr }
    })
    .filter((r) => r.addr)
}

export function successRate(stats?: Pick<Stats, "count" | "total">) {
  if (!stats || !stats.total) return 0
  return (stats.count / stats.total) * 100
}

export function formatMs(val?: number) {
  if (val === undefined || Number.isNaN(val)) return "—"
  return `${val.toFixed(1)} ms`
}

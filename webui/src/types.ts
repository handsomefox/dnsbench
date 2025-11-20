export type DNSServer = {
  name: string
  addr: string
}

export type Stats = {
  min: number
  max: number
  mean: number
  count: number
  errors: number
  total: number
}

export type BenchmarkResult = {
  server: DNSServer
  stats: Stats
}

export type RunOptions = {
  repeats: number
  timeoutMs: number
  concurrency: number
  warmup: number
  onlyMajor: boolean
}

export type DefaultsResponse = {
  resolvers: DNSServer[]
  majorResolvers: DNSServer[]
  domains: string[]
  options: RunOptions
}

export type RunRequest = {
  domains: string[]
  resolvers: DNSServer[]
  options: RunOptions
}

export type SSEMessage = {
  runId?: string
  type: string
  detail?: Record<string, unknown>
}

export type QueryLog = {
  domain: string
  server: string
  latency?: number
  error?: string
  ts: number
}

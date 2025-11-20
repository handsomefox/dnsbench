import { useMemo, useState } from "react"
import { Plus, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { InfoTooltip } from "../ui/info-tooltip"
import { NumberField } from "./NumberField"
import type { DefaultsResponse, RunOptions } from "@/types"

type Props = {
  domainsText: string
  onDomainsChange: (v: string) => void
  preparedDomainsCount: number
  options: RunOptions
  onOptionsChange: (opts: RunOptions) => void
  customResolvers: string
  onCustomResolversChange: (v: string) => void
  parsedResolversCount: number
  defaults?: DefaultsResponse | null
}

export function ConfigPanel({
  domainsText,
  onDomainsChange,
  preparedDomainsCount,
  options,
  onOptionsChange,
  customResolvers,
  onCustomResolversChange,
  parsedResolversCount,
  defaults,
}: Props) {
  const resolverList =
    options.onlyMajor ? defaults?.majorResolvers ?? [] : defaults?.resolvers ?? []
  const domainList = useMemo(
    () =>
      domainsText
        .split("\n")
        .map((d) => d.trim())
        .filter(Boolean),
    [domainsText],
  )
  const [newDomain, setNewDomain] = useState("")
  const [showTextEditor, setShowTextEditor] = useState(false)

  const handleAddDomain = () => {
    const next = newDomain.trim()
    if (!next) return
    if (domainList.includes(next)) {
      setNewDomain("")
      return
    }
    const updated = [...domainList, next]
    onDomainsChange(updated.join("\n"))
    setNewDomain("")
  }

  const handleDeleteDomain = (domain: string) => {
    const updated = domainList.filter((d) => d !== domain)
    onDomainsChange(updated.join("\n"))
  }

  return (
    <Card className="border-muted/70 bg-card/60 shadow-sm backdrop-blur">
      <CardHeader className="pb-4">
        <CardTitle className="text-xl">Configuration</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Label>Domains</Label>
              <InfoTooltip label="Domains to resolve during the benchmark (one per entry)." />
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowTextEditor((v) => !v)}
              className="text-xs"
            >
              {showTextEditor ? "Hide text editor" : "Edit as text"}
            </Button>
          </div>
          <div className="flex gap-3">
            <Input
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              placeholder="example.com"
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  handleAddDomain()
                }
              }}
            />
            <Button onClick={handleAddDomain} className="shrink-0">
              <Plus className="mr-2 h-4 w-4" />
              Add
            </Button>
          </div>
          <ScrollArea className="h-40 rounded-lg border bg-muted/30 p-3">
            {domainList.length === 0 ? (
              <p className="text-xs text-muted-foreground">No domains yet. Add one above.</p>
            ) : (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3">
                {domainList.map((d) => (
                  <div
                    key={d}
                    className="flex items-center justify-between rounded-md border bg-background/70 px-3 py-2 text-sm shadow-sm"
                  >
                    <span className="truncate">{d}</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Remove ${d}`}
                      onClick={() => handleDeleteDomain(d)}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </ScrollArea>
          {showTextEditor && (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">Plain text editor</Label>
              <Textarea
                value={domainsText}
                onChange={(e) => onDomainsChange(e.target.value)}
                placeholder="one.domain.test per line"
                className="min-h-[140px]"
              />
              <p className="text-xs text-muted-foreground">
                {preparedDomainsCount} domain{preparedDomainsCount === 1 ? "" : "s"} will be tested.
              </p>
            </div>
          )}
        </div>

          <Tabs defaultValue="defaults" className="w-full">
            <TabsList className="grid grid-cols-2">
              <TabsTrigger value="defaults">Defaults</TabsTrigger>
              <TabsTrigger value="custom">Custom</TabsTrigger>
            </TabsList>
            <TabsContent value="defaults" className="space-y-3 pt-3">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <p className="text-sm font-medium">Built-in resolvers</p>
                  <p className="text-xs text-muted-foreground">
                    {options.onlyMajor ? "Major providers only" : "Full curated list"}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Label htmlFor="onlyMajor" className="text-xs text-muted-foreground">
                    Major only
                  </Label>
                  <InfoTooltip label="When on, limits benchmarks to the major resolver set." />
                  <Switch
                    id="onlyMajor"
                    checked={options.onlyMajor}
                    onCheckedChange={(checked) => onOptionsChange({ ...options, onlyMajor: checked })}
                  />
                </div>
              </div>
            <ScrollArea className="h-40 rounded-lg border bg-muted/30 p-3">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {resolverList.length ? (
                  resolverList.map((r) => (
                    <div
                      key={r.addr}
                      className="rounded-md border bg-background/70 px-3 py-2 text-sm shadow-sm"
                    >
                      <p className="font-semibold text-foreground">{r.name}</p>
                      <p className="text-xs text-muted-foreground">{r.addr}</p>
                    </div>
                  ))
                ) : (
                  <span className="text-muted-foreground">Loading resolvers…</span>
                )}
              </div>
            </ScrollArea>
            </TabsContent>
            <TabsContent value="custom" className="space-y-2 pt-3">
              <Label>Custom resolvers</Label>
              <Textarea
                value={customResolvers}
              onChange={(e) => onCustomResolversChange(e.target.value)}
              placeholder="cloudflare;1.1.1.1"
              className="min-h-[120px]"
            />
            <p className="text-xs text-muted-foreground">Format: name;ip — one per line. Leave empty to use defaults.</p>
            <p className="text-xs text-muted-foreground">Parsed: {parsedResolversCount} resolver(s)</p>
          </TabsContent>
        </Tabs>

        <Separator />

        <div className="grid grid-cols-2 gap-3">
          <NumberField
            label="Repeats"
            value={options.repeats}
            min={1}
            onChange={(v) => onOptionsChange({ ...options, repeats: v })}
            hint={<InfoTooltip label="How many times each domain is queried per resolver." />}
          />
          <NumberField
            label="Timeout (ms)"
            value={options.timeoutMs}
            min={100}
            step={100}
            onChange={(v) => onOptionsChange({ ...options, timeoutMs: v })}
            hint={<InfoTooltip label="Per-query timeout in milliseconds before treating it as failed." />}
          />
          <NumberField
            label="Concurrency"
            value={options.concurrency}
            min={1}
            onChange={(v) => onOptionsChange({ ...options, concurrency: v })}
            hint={<InfoTooltip label="Maximum simultaneous DNS queries across resolvers." />}
          />
          <NumberField
            label="Warmup runs"
            value={options.warmup}
            min={0}
            onChange={(v) => onOptionsChange({ ...options, warmup: v })}
            hint={<InfoTooltip label="Optional warmup queries per resolver/domain before timing starts." />}
          />
        </div>

        <div className="flex gap-2 text-xs text-muted-foreground">
          <Badge variant="secondary">Parsed domains: {preparedDomainsCount}</Badge>
          <Badge variant="secondary">Resolvers: {parsedResolversCount || resolverList.length}</Badge>
        </div>
      </CardContent>
    </Card>
  )
}

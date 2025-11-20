type Props = {
  label: string
  value: string
}

export function ResultStat({ label, value }: Props) {
  return (
    <div className="rounded-lg border bg-background/80 px-3 py-2">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="text-md font-semibold">{value}</p>
    </div>
  )
}

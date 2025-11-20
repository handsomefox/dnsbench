import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

type Props = {
  label: string
  value: number
  min: number
  step?: number
  onChange: (v: number) => void
  hint?: React.ReactNode
}

export function NumberField({ label, value, min, step = 1, onChange, hint }: Props) {
  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <Label className="text-xs text-muted-foreground">{label}</Label>
        {hint}
      </div>
      <Input
        type="number"
        value={value}
        min={min}
        step={step}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </div>
  )
}

import { Info } from "lucide-react"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

type Props = {
  label: string
}

export function InfoTooltip({ label }: Props) {
  return (
    <TooltipProvider delayDuration={100}>
      <Tooltip>
        <TooltipTrigger className="text-muted-foreground">
          <Info className="h-4 w-4" aria-hidden />
          <span className="sr-only">{label}</span>
        </TooltipTrigger>
        <TooltipContent
          side="top"
          className="max-w-xs text-xs leading-snug bg-popover text-popover-foreground"
        >
          {label}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

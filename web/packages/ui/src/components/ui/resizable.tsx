"use client"

import { useRef, useState } from "react"
import * as ResizablePrimitive from "react-resizable-panels"

import { cn } from "@/lib/utils"

/**
 * Matches paseo's resize-handle: the highlight only appears after a short
 * hover, so brushing past the handle on the way to something else doesn't
 * flicker it on.
 */
const HANDLE_HOVER_DELAY_MS = 150

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      className={cn(
        "flex h-full w-full aria-[orientation=vertical]:flex-col",
        className
      )}
      {...props}
    />
  )
}

function ResizablePanel({ ...props }: ResizablePrimitive.PanelProps) {
  return <ResizablePrimitive.Panel data-slot="resizable-panel" {...props} />
}

function ResizableHandle({
  withHandle,
  className,
  onPointerEnter,
  onPointerLeave,
  onPointerDown,
  onPointerUp,
  onPointerCancel,
  ...props
}: ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean
}) {
  const [highlighted, setHighlighted] = useState(false)
  const hoverTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearHoverTimer = () => {
    if (hoverTimerRef.current !== null) {
      clearTimeout(hoverTimerRef.current)
      hoverTimerRef.current = null
    }
  }

  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      data-highlighted={highlighted ? "" : undefined}
      onPointerEnter={(event) => {
        clearHoverTimer()
        hoverTimerRef.current = setTimeout(() => setHighlighted(true), HANDLE_HOVER_DELAY_MS)
        onPointerEnter?.(event)
      }}
      onPointerLeave={(event) => {
        clearHoverTimer()
        setHighlighted(false)
        onPointerLeave?.(event)
      }}
      onPointerDown={(event) => {
        clearHoverTimer()
        setHighlighted(true)
        onPointerDown?.(event)
      }}
      onPointerUp={(event) => {
        setHighlighted(false)
        onPointerUp?.(event)
      }}
      onPointerCancel={(event) => {
        setHighlighted(false)
        onPointerCancel?.(event)
      }}
      className={cn(
        // z-20 keeps the handle's hit area above shadcn's Sidebar, whose
        // fixed-position `sidebar-container` (z-10, see sidebar.tsx) sits
        // flush against this boundary -- without it, a real browser routes
        // pointerdown on the overlapping pixels to the sidebar instead of
        // this separator, and react-resizable-panels' own occlusion check
        // (correctly) refuses to treat that as a resize gesture, leaving a
        // dead zone along part of the drag handle.
        "relative z-20 flex w-px items-center justify-center bg-border ring-offset-background after:absolute after:inset-y-0 after:left-1/2 after:w-1 after:-translate-x-1/2 after:bg-transparent focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden data-[highlighted]:after:bg-accent aria-[orientation=horizontal]:h-px aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:after:left-0 aria-[orientation=horizontal]:after:h-1 aria-[orientation=horizontal]:after:w-full aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2 [&[aria-orientation=horizontal]>div]:rotate-90",
        className
      )}
      {...props}
    >
      {withHandle && (
        <div
          className={cn(
            "z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border transition-colors",
            highlighted && "bg-accent"
          )}
        />
      )}
    </ResizablePrimitive.Separator>
  )
}

export { ResizableHandle, ResizablePanel, ResizablePanelGroup }

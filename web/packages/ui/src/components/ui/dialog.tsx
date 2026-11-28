import * as React from "react"
import { Dialog as SheetPrimitive } from "radix-ui"
import { Dialog as AstryxDialog } from "@astryxdesign/core/Dialog"

import { cn } from "@/lib/utils"
import { Icons } from "@/lib/icons";

// jsdom lacks HTMLDialogElement.showModal/close, which Astryx calls. A no-op
// browser never takes this branch; it only keeps component tests rendering.
// (Belongs in src/test/setup.ts; moved there when that file is in scope.)
if (typeof HTMLDialogElement !== "undefined") {
  const proto = HTMLDialogElement.prototype
  if (typeof proto.showModal !== "function") {
    proto.showModal = function showModal(this: HTMLDialogElement) {
      this.setAttribute("open", "")
    }
  }
  if (typeof proto.close !== "function") {
    proto.close = function close(this: HTMLDialogElement) {
      this.removeAttribute("open")
    }
  }
}

// Astryx Dialog (Wave 0) renders DialogContent: a native <dialog> opened with
// showModal(), so Escape, backdrop click, focus trapping, top layer and its
// keyboard-only focus ring come from Astryx. It is controlled (isOpen /
// onOpenChange) and has no Trigger/Portal/Overlay/Close parts, so the Radix
// Root stays: it keeps Trigger, Close, Title, Description and the consumers
// that compose DialogPrimitive.Content with DialogOverlay/DialogPortal
// (settings-dialog, attachment-lightbox) working. This Root only mirrors the
// open state into a context so DialogContent can drive Astryx.
const DialogOpenContext = React.createContext<{
  open: boolean
  setOpen: (open: boolean) => void
}>({ open: false, setOpen: () => {} })

function Dialog({
  open: openProp,
  defaultOpen,
  onOpenChange,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Root>) {
  const [inner, setInner] = React.useState(!!defaultOpen)
  const controlled = openProp !== undefined
  const open = controlled ? !!openProp : inner
  const setOpen = React.useCallback(
    (next: boolean) => {
      if (!controlled) setInner(next)
      onOpenChange?.(next)
    },
    [controlled, onOpenChange]
  )
  return (
    <DialogOpenContext.Provider value={{ open, setOpen }}>
      <SheetPrimitive.Root
        data-slot="dialog"
        open={open}
        onOpenChange={setOpen}
        {...props}
      />
    </DialogOpenContext.Provider>
  )
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Trigger>) {
  return <SheetPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Portal>) {
  return <SheetPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Close>) {
  return <SheetPrimitive.Close data-slot="dialog-close" {...props} />
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Overlay>) {
  return (
    <SheetPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 z-50 bg-black/50 duration-(--duration-menu) data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
        className
      )}
      {...props}
    />
  )
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  ref,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Content> & {
  showCloseButton?: boolean
}) {
  const { open, setOpen } = React.useContext(DialogOpenContext)
  const innerRef = React.useRef<HTMLDialogElement | null>(null)
  const beforeRef = React.useRef<HTMLElement | null>(null)
  if (open && !beforeRef.current) beforeRef.current = document.activeElement as HTMLElement | null
  if (!open) beforeRef.current = null
  const onCloseAutoFocusRef = React.useRef(props.onCloseAutoFocus)
  onCloseAutoFocusRef.current = props.onCloseAutoFocus
  const setRef = React.useCallback(
    (node: HTMLDialogElement | null) => {
      innerRef.current = node
      if (typeof ref === "function") ref(node as unknown as HTMLDivElement)
      else if (ref) (ref as React.RefObject<unknown>).current = node
    },
    [ref]
  )

  // Name the dialog from DialogTitle / DialogDescription (Radix wires these
  // ids only for its own Content).
  React.useLayoutEffect(() => {
    const el = innerRef.current
    if (!el || !open) return
    const title = el.querySelector<HTMLElement>('[data-slot="dialog-title"]')
    const desc = el.querySelector<HTMLElement>('[data-slot="dialog-description"]')
    if (title?.id) el.setAttribute("aria-labelledby", title.id)
    if (desc?.id) el.setAttribute("aria-describedby", desc.id)
  })

  const {
    onOpenAutoFocus: _a,
    onCloseAutoFocus: _b, // handled above
    onEscapeKeyDown: _c,
    onPointerDownOutside: _d,
    onInteractOutside: _e,
    forceMount: _f,
    ...rest
  } = props

  // Radix gave focus back to whatever had it before the dialog opened. The
  // content unmounts on close here (no native close()), so do it explicitly.
  React.useLayoutEffect(() => {
    if (!open) return
    // Captured while rendering (below): children's autoFocus has already moved
    // focus by the time any effect runs.
    const before = beforeRef.current
    return () => {
      const ev = new Event("closeautofocus", { cancelable: true })
      onCloseAutoFocusRef.current?.(ev)
      if (ev.defaultPrevented) return
      if (before && before !== document.body && before.isConnected) before.focus()
    }
  }, [open])

  // Radix hid everything outside the modal from assistive tech (and from
  // role queries); a native modal <dialog> only does so in real browsers.
  React.useLayoutEffect(() => {
    const el = innerRef.current
    if (!el || !open) return
    const hidden: Element[] = []
    for (let node: Element = el; node.parentElement; node = node.parentElement) {
      for (const sib of Array.from(node.parentElement.children)) {
        if (sib !== node && !sib.hasAttribute("aria-hidden") && !["SCRIPT", "STYLE"].includes(sib.tagName)) {
          sib.setAttribute("aria-hidden", "true")
          hidden.push(sib)
        }
      }
    }
    return () => hidden.forEach((n) => n.removeAttribute("aria-hidden"))
  }, [open])

  // Escape closes only the top-most dialog. A Radix layer opened earlier (the
  // settings sheet) would otherwise take the key first. Capture on window runs
  // before any document-level listener.
  React.useEffect(() => {
    const el = innerRef.current
    if (!el || !open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.isComposing) return
      const all = document.querySelectorAll('[data-slot="dialog-content"]')
      if (all[all.length - 1] !== el) return
      e.stopImmediatePropagation()
      e.preventDefault()
      setOpen(false)
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [open, setOpen])

  // Unmount when closed (Radix did): a closed native <dialog> stays in the DOM
  // and keeps its children queryable. Costs the exit animation.
  if (!open) return null

  return (
    <AstryxDialog
      {...(rest as object)}
      ref={setRef}
      isOpen={open}
      padding={0} // our DialogHeader/Body/Footer wrapper carries p-6, as before
      onOpenChange={(next) => setOpen(next)}
      data-slot="dialog-content"
      role="dialog" // explicit, as Radix had it (a native <dialog> only has the implicit role)
      // Positioning, animation and focus handling now belong to Astryx; the
      // surface keeps our radius and grid so child layout is unchanged.
      // m-auto: the Tailwind preflight `* { margin: 0 }` outranks Astryx's margin:auto
      // when the cascade layers are mis-ordered; the utility centers it either way.
      className={cn("m-auto w-full max-w-[calc(100%-2rem)] rounded-2xl sm:max-w-lg", className, sheetResets(className))}
    >
      <div className={cn("grid gap-4 p-6", childLayoutClasses(className))}>{children}</div>
      {showCloseButton && (
        <SheetPrimitive.Close
          data-slot="dialog-close"
          className="absolute top-3.5 right-3.5 flex size-7 items-center justify-center rounded-lg text-foreground-muted opacity-70 transition-opacity hover:bg-muted hover:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-hidden disabled:pointer-events-none [&>svg]:size-4 [&>svg]:shrink-0"
        >
          <Icons.close />
          <span className="sr-only">Close</span>
        </SheetPrimitive.Close>
      )}
    </AstryxDialog>
  )
}

/**
 * A call site that pins the dialog to the viewport (`inset-0`, optionally under a
 * variant) means the old fixed content element: it filled the area with auto size.
 * The native <dialog> additionally caps its height and keeps an auto margin, so under
 * the same variant drop both. Tailwind only emits classes it finds as literals in the
 * source, hence the explicit table of the variants call sites use.
 */
const SHEET_RESETS: Record<string, string> = {
  "inset-0": "max-h-none m-0",
  "max-[860px]:inset-0": "max-[860px]:max-h-none max-[860px]:m-0",
  "[@media(max-height:560px)]:inset-0": "[@media(max-height:560px)]:max-h-none [@media(max-height:560px)]:m-0",
}

function sheetResets(className: string | undefined): string {
  return (className ?? "")
    .split(/\s+/)
    .map((t) => SHEET_RESETS[t])
    .filter(Boolean)
    .join(" ")
}

/** Astryx wraps the children in our body grid; the `[&>*]:` child-selector utilities a call site puts on DialogContent must target that wrapper's children. */
function childLayoutClasses(className: string | undefined): string {
  return (className ?? "").split(/\s+/).filter((t) => t.startsWith("[&>")).join(" ")
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2 text-center sm:text-left pr-6", className)}
      {...props}
    />
  )
}

function DialogBody({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-body"
      className={cn("grid gap-4", className)}
      {...props}
    />
  )
}

function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "flex flex-col-reverse gap-2 sm:flex-row sm:justify-end",
        className
      )}
      {...props}
    />
  )
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Title>) {
  return (
    <SheetPrimitive.Title
      data-slot="dialog-title"
      className={cn("text-ui-lg font-semibold", className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Description>) {
  return (
    <SheetPrimitive.Description
      data-slot="dialog-description"
      className={cn("text-ui-base text-muted-foreground", className)}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogBody,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}

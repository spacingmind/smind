import * as React from "react"
import { TextInput } from "@astryxdesign/core/TextInput"

import { cn } from "@/lib/utils"

// Astryx TextInput (Wave 0) renders the text-like types. It is a controlled
// field with a required label, so this wrapper:
//   - keeps our event-style onChange(e) (Astryx passes (value, e)),
//   - tracks its own value when the caller passes none (uncontrolled use),
//   - hides the Astryx label and names the input from aria-label/placeholder.
// Astryx supports only text|password|email|search|tel|url. Other types
// (number, file, date, ...) cannot be expressed, so they keep the previous
// native <input> with the Tailwind classes below.
const ASTRYX_TYPES = new Set(["text", "password", "email", "search", "tel", "url"])

const nativeClasses =
  "h-8 w-full min-w-0 rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-lg transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-ui-base file:font-medium file:text-foreground placeholder:text-foreground-subtlest hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-ui-base dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  if (type && !ASTRYX_TYPES.has(type)) {
    return <input type={type} data-slot="input" className={cn(nativeClasses, className)} {...props} />
  }
  return <AstryxInput type={type} className={className} {...props} />
}

// Astryx TextInput takes no className for the <input> element itself, only for the
// field container. Text-level utilities (font, size, colour, tracking, ellipsis) belong
// on the text element, so they are applied to the <input>; everything else (sizing,
// flex, margin) lands on the container.
// Same text look as the native input: size and grey placeholder.
const TEXT_DEFAULTS = "text-ui-base placeholder:text-foreground-subtlest"
// Same field box as the native input (h-8, 1px border, rounded-lg, px-2.5, fill). Utilities
// win over Astryx's cascade layers, so the look does not depend on its field styles.
// shadow-none drops Astryx's 2px inset focus/hover ring: focus is the border + fill change.
const CONTROL_CLASSES =
  "h-8 rounded-lg border border-input-border bg-input px-2.5 shadow-none hover:shadow-none focus-within:shadow-none hover:border-input-border-hover focus-within:border-input-border-focused focus-within:bg-input-focused"
const TEXT_LEVEL = /^(font-|text-|tracking-|leading-|placeholder:)/
function splitClassName(className: string | undefined): { text: string; box: string } {
  const text: string[] = []
  const box: string[] = []
  for (const token of (className ?? "").split(/\s+/).filter(Boolean)) (TEXT_LEVEL.test(token) ? text : box).push(token)
  return { text: text.join(" "), box: box.join(" ") }
}

function AstryxInput({
  className,
  type,
  value,
  defaultValue,
  onChange,
  disabled,
  readOnly,
  placeholder,
  autoFocus,
  ref,
  "aria-invalid": ariaInvalid,
  "aria-busy": ariaBusy,
  ...rest
}: React.ComponentProps<"input">) {
  const [inner, setInner] = React.useState(String(defaultValue ?? ""))
  const controlled = value !== undefined
  const controlledRef = React.useRef(controlled)
  controlledRef.current = controlled
  const current = controlled ? String(value) : inner
  const invalid = ariaInvalid === true || ariaInvalid === "true"
  const inputRef = React.useRef<HTMLInputElement | null>(null)
  const mountedRef = React.useRef(false)
  // React mirrors a controlled value into the value attribute; a password must
  // never sit in the DOM (a native uncontrolled input has no such attribute).
  const { text: textClass, box: boxClass } = splitClassName(cn(TEXT_DEFAULTS, className))
  React.useLayoutEffect(() => {
    const el = inputRef.current
    if (!el) return
    if (!mountedRef.current) {
      mountedRef.current = true
      if (autoFocus) {
        // Dialog owns focus after showModal; focusing at mount causes an early blur.
        el.setAttribute("data-autofocus", "")
        if (el.closest("dialog") === null) el.focus()
      }
    }
    if (textClass) el.classList.add(...textClass.split(" "))
    if (type === "password") el.removeAttribute("value")
    // Astryx writes aria-invalid itself (from `status`, unused here).
    // Same for aria-busy (Astryx derives it from isLoading).
    if (ariaBusy === undefined || ariaBusy === false) el.removeAttribute("aria-busy")
    else el.setAttribute("aria-busy", String(ariaBusy))
    if (invalid) el.setAttribute("aria-invalid", "true")
    else if (ariaInvalid === undefined || ariaInvalid === false) el.removeAttribute("aria-invalid")
    else el.setAttribute("aria-invalid", String(ariaInvalid))
  })
  const setRef = React.useCallback(
    (node: HTMLInputElement | null) => {
      inputRef.current = node
      // Uncontrolled callers (e.g. clearing a field with `el.value = ""`) write
      // the DOM value directly; mirror such writes into our state, otherwise
      // the controlled Astryx field would put the old text back.
      if (node && !controlledRef.current && !(node as { __smindSync?: boolean }).__smindSync) {
        ;(node as { __smindSync?: boolean }).__smindSync = true
        const own = Object.getOwnPropertyDescriptor(node, "value")
        const proto = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")
        const base = own ?? proto
        if (base?.get && base.set) {
          Object.defineProperty(node, "value", {
            configurable: true,
            get() {
              return base.get!.call(this)
            },
            set(v: string) {
              base.set!.call(this, v)
              setInner(String(v))
            },
          })
        }
      }
      if (typeof ref === "function") ref(node)
      else if (ref) (ref as React.RefObject<HTMLInputElement | null>).current = node
    },
    [ref]
  )
  return (
    <TextInput
      {...(rest as object)}
      ref={setRef}
      data-slot="input"
      type={type as "text" | undefined}
      // Astryx needs a label but renders its text in the DOM (a second copy of
      // any visible label, found by getByText). A non-breaking space keeps it
      // empty; the name comes from aria-label (in rest), a real <label>, or the
      // placeholder.
      label={"\u00a0"}
      isLabelHidden
      value={current}
      placeholder={placeholder}
      isDisabled={disabled}
      // Astryx disables with aria-disabled; forward the native attribute too
      // (it reaches the <input> through the field's rest props).
      {...({ disabled } as object)}
      isReadOnly={readOnly}
      // Not Astryx `status`: it adds a live region (second role=alert) and
      // overwrites aria-invalid. aria-invalid goes straight to the <input>.
      aria-invalid={invalid ? true : ariaInvalid}
      controlClassName={cn(CONTROL_CLASSES, "has-[[aria-invalid=true]]:border-destructive has-[[aria-invalid=true]]:ring-3 has-[[aria-invalid=true]]:ring-destructive/20")}
      onChange={(next, e) => {
        if (!controlled) setInner(next)
        onChange?.(e)
      }}
      // Sizing utilities from call sites land on the field container.
      containerClassName={cn(boxClass)}
    />
  )
}

export { Input }

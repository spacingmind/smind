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

function AstryxInput({
  className,
  type,
  value,
  defaultValue,
  onChange,
  disabled,
  readOnly,
  placeholder,
  ref,
  "aria-label": ariaLabel,
  "aria-invalid": ariaInvalid,
  ...rest
}: React.ComponentProps<"input">) {
  const [inner, setInner] = React.useState(String(defaultValue ?? ""))
  const controlled = value !== undefined
  const current = controlled ? String(value) : inner
  const invalid = ariaInvalid === true || ariaInvalid === "true"
  return (
    <TextInput
      {...(rest as object)}
      ref={ref}
      data-slot="input"
      type={type as "text" | undefined}
      label={ariaLabel ?? placeholder ?? "Input"}
      isLabelHidden
      value={current}
      placeholder={placeholder}
      isDisabled={disabled}
      isReadOnly={readOnly}
      status={invalid ? { type: "error", message: "Invalid value" } : undefined}
      statusVariant="tooltip"
      onChange={(next, e) => {
        if (!controlled) setInner(next)
        onChange?.(e)
      }}
      // Sizing utilities from call sites land on the field container.
      containerClassName={cn(className)}
    />
  )
}

export { Input }

import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"
import { Switch as AstryxSwitch } from "@astryxdesign/core/Switch"

import { cn } from "@/lib/utils"

// Astryx Switch (Wave 0) renders the control. It is a controlled field
// (`value: boolean`, `label` required) whose role=switch element is a native
// checkbox <input>, so this wrapper:
//   - keeps the Radix prop names (checked/defaultChecked/onCheckedChange/
//     disabled/name/required/aria-label/aria-labelledby) and tracks its own
//     state when uncontrolled,
//   - hides the Astryx label and uses aria-label as the accessible name,
//   - mirrors aria-checked and data-state (checked|unchecked) onto the input
//     (Astryx only sets the native `checked`), keeping Radix's DOM contract.
// Astryx disables with aria-disabled (still focusable) instead of `disabled`.
type SwitchProps = Omit<React.ComponentProps<typeof SwitchPrimitive.Root>, "asChild">

function Switch({
  className,
  checked,
  defaultChecked,
  onCheckedChange,
  disabled,
  name,
  required,
  id,
  value: _value,
  ref,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  ...rest
}: SwitchProps) {
  const [inner, setInner] = React.useState(!!defaultChecked)
  const controlled = checked !== undefined
  const on = controlled ? !!checked : inner
  const inputRef = React.useRef<HTMLInputElement | null>(null)

  React.useLayoutEffect(() => {
    const el = inputRef.current
    if (!el) return
    el.setAttribute("aria-checked", String(on))
    el.setAttribute("data-state", on ? "checked" : "unchecked")
    // Astryx disables with aria-disabled only; mirror Radix's native attribute.
    el.disabled = !!disabled
  })

  const setRef = React.useCallback(
    (node: HTMLInputElement | null) => {
      inputRef.current = node
      if (typeof ref === "function") ref(node as unknown as HTMLButtonElement)
      else if (ref) (ref as React.RefObject<unknown>).current = node
    },
    [ref]
  )

  return (
    <AstryxSwitch
      {...(rest as object)}
      ref={setRef}
      id={id}
      data-slot="switch"
      data-state={on ? "checked" : "unchecked"}
      label={ariaLabel ?? "Toggle"}
      isLabelHidden
      aria-labelledby={ariaLabelledBy}
      value={on}
      isDisabled={disabled}
      isRequired={required}
      htmlName={name}
      onChange={(next) => {
        if (!controlled) setInner(next)
        onCheckedChange?.(next)
      }}
      className={cn("peer", className)}
    />
  )
}

export { Switch }

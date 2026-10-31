interface HomeIllustrationProps {
  className?: string;
}

/** Abstract line-art window with a cursor; drawn in currentColor so it follows the theme. */
export function HomeIllustration({ className = "h-24 w-40" }: HomeIllustrationProps) {
  return (
    <svg
      viewBox="0 0 160 96"
      aria-hidden="true"
      focusable="false"
      data-testid="home-illustration"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`text-foreground-subtlest ${className}`}
    >
      <rect x="28" y="14" width="88" height="62" rx="8" />
      <path d="M28 28h88" />
      <circle cx="38" cy="21" r="1.5" />
      <circle cx="45" cy="21" r="1.5" />
      <circle cx="52" cy="21" r="1.5" />
      <path d="M40 40h44M40 50h30" />
      <path d="M40 62h10" strokeWidth={2.5} />
      <path d="M130 14v6M141 24h-6M137 17l-4 4" />
      <path d="M12 86h136" opacity={0.5} />
    </svg>
  );
}

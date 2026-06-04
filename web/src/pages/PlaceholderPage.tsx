interface Props { title: string; description?: string }

export function PlaceholderPage({ title, description }: Props) {
  return (
    <div>
      <h1 className="text-xl font-semibold mb-1" style={{ color: 'var(--text)' }}>{title}</h1>
      <p style={{ color: 'var(--text-muted)' }}>{description ?? 'Coming in a future milestone.'}</p>
    </div>
  )
}

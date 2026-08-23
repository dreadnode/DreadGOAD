// Azure resource IDs are ~200 characters of subscription and provider path.
// The last segment is the part an operator reads.
export function shortResourceId(id: string): string {
  const parts = id.split('/')
  return parts[parts.length - 1] || id
}

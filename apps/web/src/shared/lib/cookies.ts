export function getCookie(name: string): string | undefined {
  const pair = document.cookie
    .split("; ")
    .find((part) => part.startsWith(name + "="));
  return pair?.slice(name.length + 1);
}

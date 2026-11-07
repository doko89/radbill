export type WebpOptions = {
  /** Longest edge in px; larger images are downscaled. Defaults to 512. */
  maxDimension?: number;
  /** WebP quality 0..1. Defaults to 0.8. */
  quality?: number;
};

/** Converts an image file to WebP via canvas. The server re-validates the output. */
export async function fileToWebp(file: File, opts: WebpOptions = {}): Promise<Blob> {
  const { maxDimension = 512, quality = 0.8 } = opts;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new Error("file is not a readable image");
  }
  try {
    const scale = Math.min(1, maxDimension / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      throw new Error("canvas 2d unavailable");
    }
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/webp", quality));
    if (!blob) {
      throw new Error("webp conversion failed");
    }
    return blob;
  } finally {
    bitmap.close();
  }
}

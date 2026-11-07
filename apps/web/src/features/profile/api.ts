import { z } from "zod";
import { apiForm } from "../../shared/lib/api";
import { fileToWebp } from "../../shared/lib/image";

const uploadSchema = z.object({ url: z.string() });

/** Converts the picked file to WebP (80% quality) and uploads it as avatar. */
export async function uploadAvatar(file: File): Promise<string> {
  const webp = await fileToWebp(file, { quality: 0.8 });
  const form = new FormData();
  form.append("file", webp, "avatar.webp");
  const res: unknown = await apiForm("/api/uploads/avatar", form);
  return uploadSchema.parse(res).url;
}

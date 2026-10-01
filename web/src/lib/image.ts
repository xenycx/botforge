/** Center-crops an image file to a square and returns it as a base64 PNG
 * (`size` pixels a side), keeping transparency. Throws when the file cannot
 * be decoded. */
export async function squarePng(file: File, size = 256): Promise<{ base64: string; dataUrl: string }> {
	if (file.size > 8 * 1024 * 1024) throw new Error('Choose an image smaller than 8 MB.');
	const bitmap = await createImageBitmap(file);
	const side = Math.min(bitmap.width, bitmap.height);
	const out = Math.min(size, Math.max(16, side));
	const canvas = document.createElement('canvas');
	canvas.width = canvas.height = out;
	const g = canvas.getContext('2d')!;
	g.imageSmoothingQuality = 'high';
	g.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, out, out);
	bitmap.close();
	let dataUrl = canvas.toDataURL('image/png');
	// Photos compress poorly as PNG; fall back to JPEG to stay under the limit.
	if ((dataUrl.length * 3) / 4 > 240 * 1024) dataUrl = canvas.toDataURL('image/jpeg', 0.85);
	return { base64: dataUrl.split(',')[1], dataUrl };
}

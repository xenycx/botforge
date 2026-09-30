// Renders og.html to og-image.png with the local Chromium (see tests/e2e).
import { chromium } from 'playwright-core';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
const exe = process.env.CHROMIUM ?? join(homedir(), '.cache/ms-playwright/chromium-1243/chrome-linux64/chrome');
const browser = await chromium.launch({ executablePath: exe });
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
await page.goto('file://' + resolve('og.html'));
await page.evaluate(() => document.fonts.ready);
await page.screenshot({ path: process.argv[2] ?? 'og-image.png' });
await browser.close();

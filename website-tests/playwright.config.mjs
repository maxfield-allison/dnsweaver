import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: '.', testMatch: 'site.spec.mjs', workers: 1, retries: 0,
  timeout: 120000,
  use: {
    baseURL: 'http://127.0.0.1:38113', reducedMotion: 'reduce', trace: 'retain-on-failure',
    launchOptions: process.env.CHROMIUM_PATH ? {executablePath: process.env.CHROMIUM_PATH, args: ['--use-angle=swiftshader','--enable-unsafe-swiftshader']} : undefined,
  },
  webServer: {command: 'python3 -m http.server 38113 --bind 127.0.0.1 --directory ../build/website', url: 'http://127.0.0.1:38113', reuseExistingServer: false, timeout: 15000, stdout: 'ignore', stderr: 'ignore'},
});

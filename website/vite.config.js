import { defineConfig } from 'vite'
import { resolve } from 'path'

export default defineConfig({
  base: '/global-sys-utils/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        index:    resolve(__dirname, 'index.html'),
        about:    resolve(__dirname, 'about.html'),
        features: resolve(__dirname, 'features.html'),
        projects: resolve(__dirname, 'projects.html'),
        blog:     resolve(__dirname, 'blog.html'),
        contact:  resolve(__dirname, 'contact.html'),
        privacy:  resolve(__dirname, 'privacy.html'),
        terms:    resolve(__dirname, 'terms.html'),
        '404':                        resolve(__dirname, '404.html'),
        'blog-v2-2-0-release':        resolve(__dirname, 'blog-v2-2-0-release.html'),
        'blog-arm64-support':         resolve(__dirname, 'blog-arm64-support.html'),
        'blog-disk-pressure-guard':   resolve(__dirname, 'blog-disk-pressure-guard.html'),
        'blog-encryption-walkthrough':resolve(__dirname, 'blog-encryption-walkthrough.html'),
        'blog-vs-logrotate':          resolve(__dirname, 'blog-vs-logrotate.html'),
        'blog-gcs-backup-gke':        resolve(__dirname, 'blog-gcs-backup-gke.html'),
      }
    }
  }
})

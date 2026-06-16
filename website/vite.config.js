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
        '404':      resolve(__dirname, '404.html'),
        'blog-post': resolve(__dirname, 'blog-post.html'),
      }
    }
  }
})

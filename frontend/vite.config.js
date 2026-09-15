import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) }
  },
  base: '/',
  build: {
    outDir: '../backend/web/dist',
    emptyOutDir: true,
    // v0.28.0：Tiptap(ProseMirror) 约 330KB，若与 Workspace 打在同一 chunk
    // 会让该路由首屏体积翻倍。拆成独立 chunk 后：
    //   ① 浏览器可长期缓存（编辑器代码很少变）
    //   ② Workspace 改动不再导致整包失效
    rollupOptions: {
      output: {
        // 注意：naive-ui 内部会 import vue，若把 vue 单独划到 vendor，
        // rollup 会报 naive -> vendor -> naive 的循环 chunk。
        // 因此这里把 naive-ui 归到 vendor 同一组，只把 tiptap 单独拆出来。
        manualChunks(id) {
          if (id.includes('node_modules/@tiptap') || id.includes('node_modules/prosemirror')) {
            return 'tiptap'
          }
          if (
            id.includes('node_modules/vue') ||
            id.includes('node_modules/@vue') ||
            id.includes('node_modules/pinia') ||
            id.includes('node_modules/naive-ui') ||
            id.includes('node_modules/@css-render') ||
            id.includes('node_modules/css-render') ||
            id.includes('node_modules/@juggle') ||
            id.includes('node_modules/seemly') ||
            id.includes('node_modules/date-fns') ||
            id.includes('node_modules/vooks')
          ) {
            return 'vendor'
          }
        }
      }
    }
  },
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8080' }
  }
})

<template>
  <div class="backup">
    <h2 class="page-title">系统备份与还原</h2>

    <!-- 立即备份 + 列表 -->
    <section class="panel">
      <div class="panel-head">
        <h3 class="section-title">备份列表</h3>
        <button v-if="auth.isSuper" class="btn primary" :disabled="creating" @click="doBackup">
          {{ creating ? '备份中…' : '立即备份' }}
        </button>
      </div>
      <p v-if="!auth.isSuper" class="section-sub">仅超级管理员可操作备份与还原</p>

      <div class="table">
        <div class="thead">
          <span>名称</span><span>创建时间</span><span>大小</span><span>类型</span><span class="op">操作</span>
        </div>
        <div v-for="b in backups" :key="b.id" class="trow">
          <span class="mono">{{ b.name }}</span>
          <span class="dim">{{ b.created_at }}</span>
          <span class="dim">{{ fmtSize(b.size) }}</span>
          <span>
            <span class="chip">{{ b.type === 'auto' ? '自动' : '手动' }}</span>
            <span v-if="b.remote" class="chip remote">异地</span>
          </span>
          <span class="op">
            <button class="del" @click="download(b)">下载</button>
            <button v-if="auth.isSuper" class="del warn" @click="restore(b)">还原</button>
            <button v-if="auth.isSuper" class="del danger" @click="remove(b)">删除</button>
          </span>
        </div>
        <div v-if="!backups.length" class="empty">暂无备份，点击「立即备份」创建</div>
      </div>
    </section>

    <!-- 自动备份配置 -->
    <section class="panel">
      <h3 class="section-title">自动备份配置</h3>
      <div class="form-col">
        <div class="fg2">
          <div>
            <label class="fld">频率</label>
            <select v-model="cfg.frequency" class="glass-input" :disabled="!auth.isSuper">
              <option value="none">关闭</option>
              <option value="daily">每天</option>
              <option value="weekly">每周</option>
            </select>
          </div>
          <div>
            <label class="fld">保留份数</label>
            <input v-model.number="cfg.retention" type="number" min="1" class="glass-input" :disabled="!auth.isSuper" />
          </div>
        </div>
        <div>
          <label class="fld">异地备份目录（可选，留空仅本地）</label>
          <input v-model="cfg.remote_dir" class="glass-input" placeholder="如 /opt/swb/remote-backup" :disabled="!auth.isSuper" />
        </div>
      </div>
      <button v-if="auth.isSuper" class="btn primary" :disabled="savingCfg" @click="saveCfg">
        {{ savingCfg ? '保存中…' : '保存配置' }}
      </button>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, getCurrentInstance } from 'vue'
import * as api from '@/api'
import { useAuthStore } from '@/store/auth'

const auth = useAuthStore()
const { proxy } = getCurrentInstance()
const $msg = proxy?.$msg

const backups = ref([])
const creating = ref(false)
const savingCfg = ref(false)
const cfg = reactive({ frequency: 'none', retention: 7, remote_dir: '' })

function toast(t, type = 'success') { $msg ? $msg[type](t) : alert(t) }
function fmtSize(n) {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + u[i]
}

async function loadList() {
  try { backups.value = await api.get('/backups') } catch (e) { toast(e.response?.data?.error || '加载失败', 'error') }
}
async function loadCfg() {
  try { Object.assign(cfg, await api.get('/backup-config')) } catch {}
}

async function doBackup() {
  creating.value = true
  try { await api.post('/backups'); toast('备份已创建'); await loadList() }
  catch (e) { toast(e.response?.data?.error || '备份失败', 'error') }
  finally { creating.value = false }
}

function download(b) { window.open('/api/backups/' + b.id + '/download') }

async function restore(b) {
  if (!confirm(`确定还原备份「${b.name}」？将覆盖当前所有数据，且不可撤销。`)) return
  try { await api.post('/backups/' + b.id + '/restore'); toast('还原成功，系统已切换至该备份'); await loadList() }
  catch (e) { toast(e.response?.data?.error || '还原失败', 'error') }
}

async function remove(b) {
  if (!confirm(`删除备份「${b.name}」？`)) return
  try { await api.del('/backups/' + b.id); toast('已删除'); await loadList() }
  catch (e) { toast(e.response?.data?.error || '删除失败', 'error') }
}

async function saveCfg() {
  savingCfg.value = true
  try { Object.assign(cfg, await api.post('/backup-config', cfg)); toast('配置已保存') }
  catch (e) { toast(e.response?.data?.error || '保存失败', 'error') }
  finally { savingCfg.value = false }
}

onMounted(async () => { await Promise.all([loadList(), loadCfg()]) })
</script>

<style scoped>
.page-title { font-size: 20px; font-weight: 700; margin: 0 0 16px; }
.panel { padding: 18px; border-radius: 16px; background: var(--glass); border: 1px solid var(--glass-border); margin-bottom: 18px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.section-title { font-size: 15px; font-weight: 600; margin: 0 0 14px; }
.section-sub { font-size: 12.5px; color: var(--text-faint); margin: 0 0 12px; }
.form-col { display: flex; flex-direction: column; gap: 14px; margin-bottom: 16px; max-width: 560px; }
.fg2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.fld { display: block; font-size: 12px; color: var(--text-faint); margin-bottom: 6px; }
.glass-input { width: 100%; padding: 9px 12px; border-radius: 11px; background: var(--overlay); border: 1px solid var(--glass-border); color: var(--text); font-size: 13.5px; outline: none; }
.btn { padding: 9px 18px; border-radius: 12px; border: 1px solid var(--glass-border); background: var(--glass-strong); color: var(--text); cursor: pointer; font-size: 13px; }
.btn.primary { background: var(--brand-grad); color: #fff; border: none; font-weight: 600; }
.btn:disabled { opacity: 0.6; cursor: default; }
.chip { font-size: 11.5px; padding: 2px 9px; border-radius: 999px; background: var(--overlay-2); color: var(--text-dim); }
.chip.remote { background: rgba(56,189,248,0.15); color: var(--accent-2); margin-left: 6px; }
.table { display: flex; flex-direction: column; }
.thead, .trow { display: grid; grid-template-columns: 2.2fr 1.6fr 1fr 1.2fr 2fr; gap: 10px; align-items: center; padding: 11px 12px; }
.thead { font-size: 12px; color: var(--text-faint); border-bottom: 1px solid var(--glass-border); }
.trow { border-bottom: 1px solid var(--hairline); font-size: 13px; }
.trow .dim { color: var(--text-faint); }
.mono { font-family: ui-monospace, monospace; font-size: 12px; word-break: break-all; }
.op { display: flex; gap: 8px; justify-content: flex-end; }
.del { padding: 5px 11px; border-radius: 9px; border: 1px solid var(--glass-border); background: transparent; color: var(--text-faint); cursor: pointer; font-size: 12px; }
.del:hover { color: var(--text); border-color: var(--glass-border-strong); }
.del.warn:hover { color: var(--accent); border-color: rgba(56,189,248,0.4); }
.del.danger:hover { color: var(--danger); border-color: rgba(225,29,72,0.4); }
.empty { padding: 24px; text-align: center; color: var(--text-faint); font-size: 13px; }
@media (max-width: 820px) { .fg2 { grid-template-columns: 1fr; } .thead, .trow { grid-template-columns: 1fr 1fr; } }
</style>

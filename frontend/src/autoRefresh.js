// 简单的自动刷新协调器：所有页面注册自己的数据回调，
// 模块维护一个 setInterval 定时器，按全局间隔（localStorage）
// 依次调用各回调。
//
// 用法（任意 .vue）：
//   import { useAutoRefresh } from '@/autoRefresh'
//   onMounted(() => useAutoRefresh(fn, true))
//   onUnmounted(() => useAutoRefresh(fn, false))
//
//   // 暂停/恢复
//   import { pauseAutoRefresh, resumeAutoRefresh } from '@/autoRefresh'

const KEY = 'gzt.autoRefresh.intervalSec' // localStorage 键：刷新间隔秒数
const DEFAULT_INTERVAL = 60 // 默认 60 秒

let callbacks = new Set()
let timer = null
let paused = false
let currentIntervalMs = readIntervalMs()

function readIntervalMs() {
  try {
    const v = parseInt(localStorage.getItem(KEY), 10)
    if (!isNaN(v) && v >= 5 && v <= 3600) return v * 1000
  } catch (e) {}
  return DEFAULT_INTERVAL * 1000
}

function start() {
  stop()
  if (paused || callbacks.size === 0) return
  timer = setInterval(() => {
    if (paused) return
    callbacks.forEach((fn) => {
      try {
        fn()
      } catch (e) {
        // 单个回调抛错不影响其他回调
        console.warn('[autoRefresh] 回调执行失败', e)
      }
    })
  }, currentIntervalMs)
}

function stop() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

export function setAutoRefreshInterval(sec) {
  const v = parseInt(sec, 10)
  if (isNaN(v) || v < 5 || v > 3600) return
  try { localStorage.setItem(KEY, String(v)) } catch (e) {}
  currentIntervalMs = v * 1000
  start()
}

export function getAutoRefreshInterval() {
  return Math.round(currentIntervalMs / 1000)
}

export function pauseAutoRefresh() {
  paused = true
}

export function resumeAutoRefresh() {
  paused = false
  start()
}

/**
 * 注册/注销自动刷新回调。
 * @param {Function} fn 数据刷新函数（无参）
 * @param {boolean} on true=注册；false=注销
 */
export function useAutoRefresh(fn, on = true) {
  if (typeof fn !== 'function') return
  if (on) {
    callbacks.add(fn)
    start()
  } else {
    callbacks.delete(fn)
    if (callbacks.size === 0) stop()
  }
}

export function hasAutoRefresh() {
  return callbacks.size > 0
}

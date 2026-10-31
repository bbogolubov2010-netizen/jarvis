<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { renderMarkdown } from './lib/markdown'

interface InstalledApp { name: string; icon: string; path: string; args: string[] }
interface CatalogStatus { count: number; updated_at: number }
interface AutoStartStatus {
  enabled: boolean
  mode: string
  run_value: string
  expected_value: string
  executable: string
  executable_exists: boolean
  startup_approved_disabled: boolean
}
interface GameStatus {
  playing: boolean
  name: string
  exe: string
  source: string
  started_at: number
  session_seconds: number
  today_seconds: number
  break_due: boolean
  candidate: boolean
}
interface Settings {
  ollama_url: string
  model: string
  system_prompt: string
  web_search: boolean
  voice_disabled: boolean
  voice_input_disabled: boolean
  wake_word_disabled: boolean
  voice_confirm_dangerous_disabled: boolean
  voice_agent_disabled: boolean
  voice_wake_word: string
  voice_language: string
  voice_device_id: string
  voice_device_name: string
  voice_whisper_model: string
  voice_city: string
  voice_latitude: number
  voice_longitude: number
  voice_location_asked: boolean
  auto_start_configured: boolean
  auto_start: boolean
  image_generation_enabled: boolean
  onboarding_shown: boolean
  autonomy_level: string
  proactive_briefing_disabled: boolean
  game_watch_disabled: boolean
  game_break_reminders_disabled: boolean
  system_prompt_version: number
  system_prompt_customized: boolean
}
interface VoiceDevice { id: string; name: string; is_default: boolean }
interface VoiceEngineStatus {
  root: string
  vosk_ready: boolean
  vosk_ready_en: boolean
  whisper_ready: boolean
  whisper_model: string
  whisper_backend: string
  integrity_error: string
}
interface VoiceInputStatus {
  state: string
  wake_available: boolean
  wake_available_en: boolean
  whisper_available: boolean
  language: string
  device_id: string
  last_transcript: string
  last_error: string
}
interface VoiceStatus {
  enabled: boolean
  state: string
  wake_word: string
  city: string
  engine: VoiceEngineStatus
  input: VoiceInputStatus
}
interface MicrophoneDiagnostics {
  devices: VoiceDevice[]
  device_count: number
  default_device: string
  default_device_id: string
  selected_device: string
  privacy_allowed: boolean
  privacy_value: string
  privacy_source: string
  active_endpoints: number
  disabled_endpoints: number
  unplugged_endpoints: number
  peak: number
  signal_detected: boolean
  verdict: string
  hints: string[]
  error: string
}
interface AppStatus { model: string; provider: string; goVersion: string; ollama_running: boolean; ollama_url: string; hotkey_active: boolean; hotkey_error: string; history_error: string; settings_error: string }
interface GPUInfo { name: string; vendor: string; vram_gb: number; integrated: boolean }
interface SetupInfo {
  ram_gb: number
  os: string
  arch: string
  cpu_name: string
  cpu_cores: number
  cpu_threads: number
  cpu_clock_ghz: number
  gpu_name: string
  gpu_vendor: string
  gpu_vram_gb: number
  gpu_integrated: boolean
  gpus?: GPUInfo[]
  disk_free_gb: number
  disk_total_gb: number
  ollama_installed: boolean
  ollama_running: boolean
}
interface ModelRecommendation {
  model: string
  size: string
  download_gb: number
  ram_required: number
  vram_required_gb: number
  cpu_cores_required: number
  device: string
  description: string
  pull_command: string
}
interface SetupPlan {
  info: SetupInfo
  recommendation: ModelRecommendation
  installed_models: string[]
  current_model: string
  selected_model: string
  current_model_installed: boolean
  recommended_installed: boolean
  needs_install: boolean
  needs_start: boolean
  needs_model: boolean
  ready: boolean
  installer_download_gb: number
}
interface SetupProgress {
  stage: string
  message: string
  percent: number
  detail?: string
}
interface ChatStreamStart { request_id: string; streaming: boolean; content?: string; clear_history?: boolean }
interface ChatStreamEvent { request_id: string; delta?: string; content?: string; error?: string }
interface HistoryMessage { role: 'user' | 'assistant'; content: string }
type StreamEventType = 'delta' | 'done' | 'error' | 'canceled'
interface BufferedStreamEvent { type: StreamEventType; event: ChatStreamEvent }

declare global {
  interface Window {
    go: {
      main: {
        App: {
          ProcessText(text: string): Promise<string>
          ChatStream(text: string): Promise<ChatStreamStart>
          CancelChatStream(requestID: string): Promise<void>
          ProcessWithScreen(text: string): Promise<string>
          GenerateImage(prompt: string, size: string): Promise<string>
          LoadImageBase64(path: string): Promise<string>
          GetSystemStatus(): Promise<AppStatus>
          GetAvailableModels(): Promise<string[]>
          GetHistory(): Promise<HistoryMessage[]>
          GetSettings(): Promise<Settings>
          SaveSettings(settings: Settings): Promise<void>
          PlayVoicePhrase(key: string): Promise<boolean>
          DetectSystem(): Promise<SetupInfo>
          GetRecommendedModel(ramGB: number): Promise<ModelRecommendation>
          GetSetupPlan(): Promise<SetupPlan>
          RunSetup(modelName: string): Promise<string>
          GetOllamaDownloadURL(): Promise<string>
          PullModel(modelName: string): Promise<string>
          CheckOllamaInstalled(): Promise<boolean>
          GetInstalledApps(): Promise<InstalledApp[]>
          GetCatalogStatus(): Promise<CatalogStatus>
          ScanApplications(): Promise<number>
          LaunchApp(name: string): Promise<string>
          SetAutoStart(enabled: boolean): Promise<boolean>
          IsAutoStart(): Promise<boolean>
          GetAutoStartStatus(): Promise<AutoStartStatus>
          TestAutoStart(): Promise<[boolean, string]>
          GetGameStatus(): Promise<GameStatus>
          GetVoiceStatus(): Promise<VoiceStatus>
          GetVoiceDevices(): Promise<VoiceDevice[]>
          GetMicrophoneDiagnostics(): Promise<MicrophoneDiagnostics>
          GetOnboardingTip(): Promise<string>
          StartVoiceInput(): Promise<void>
          StopVoiceInput(): Promise<void>
          PushToTalk(): Promise<boolean>
          SetVoiceDevice(id: string, name: string): Promise<void>
          DetectWeatherLocation(): Promise<string>
          SetWeatherCity(city: string): Promise<string>
          DismissVoiceLocation(): Promise<void>
          OpenExternal(url: string): Promise<void>
        }
      }
    }
    runtime?: {
      EventsOn(eventName: string, callback: (...data: any[]) => void): () => void
    }
  }
}

interface Message {
  id: number
  role: 'user' | 'assistant'
  content: string
  time: string
  image?: { src: string; size: string }
  streaming?: boolean
  streamId?: string
}

function errorText(error: unknown): string {
  if (error instanceof Error) return error.message
  return String(error)
}

function playVoice(key: string) {
  void window.go.main.App.PlayVoicePhrase(key).catch(() => {})
}

function now(): string {
  return new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}


// --- Chat ---
function greeting(): string {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
}
const messages = ref<Message[]>([
  { id: 0, role: 'assistant', content: greeting() + ', Sir. Systems are online and ready.', time: '' }
])
const input = ref('')
const loading = ref(false)
const historyReady = ref(false)
const activeStreamId = ref('')
const pendingStreamEvents = new Map<string, BufferedStreamEvent[]>()
let streamUnsubscribers: Array<() => void> = []
const chatRef = ref<HTMLElement | null>(null)
let msgId = 1
const showLanding = computed(() => messages.value.length === 0 || (messages.value.length === 1 && messages.value[0].id === 0))

// --- UI state ---
const showSidebar = ref(true)
const showSettings = ref(false)
const showImageModal = ref(false)

const settings = ref<Settings>({
  ollama_url: 'http://localhost:11434',
  model: 'llama3.1:8b',
  system_prompt: '',
  web_search: true,
  voice_disabled: false,
  voice_input_disabled: false,
  wake_word_disabled: false,
  voice_confirm_dangerous_disabled: false,
  voice_agent_disabled: false,
  voice_wake_word: 'привет джарвис',
  voice_language: 'auto',
  voice_device_id: '',
  voice_device_name: '',
  voice_whisper_model: 'large-v3-turbo',
  voice_city: '',
  voice_latitude: 0,
  voice_longitude: 0,
  voice_location_asked: false,
  auto_start_configured: false,
  auto_start: false,
  image_generation_enabled: false,
  onboarding_shown: false,
  autonomy_level: 'suggestions',
  proactive_briefing_disabled: false,
  game_watch_disabled: false,
  game_break_reminders_disabled: false,
  system_prompt_version: 2,
  system_prompt_customized: false
})
const availableModels = ref<string[]>([])
const status = ref<AppStatus>({ model: '', provider: '', goVersion: '', ollama_running: false, ollama_url: '', hotkey_active: false, hotkey_error: '', history_error: '', settings_error: '' })
const settingsError = ref('')
const modelOptions = computed(() => {
  const models = [...availableModels.value]
  if (settings.value.model && !models.includes(settings.value.model)) {
    models.unshift(settings.value.model)
  }
  return models
})

const imagePrompt = ref('')
const imageSize = ref('1024x1024')
const imageLoading = ref(false)

const installedApps = ref<InstalledApp[]>([])
const autoStart = ref(false)
const autoStartStatus = ref<AutoStartStatus | null>(null)
const autoStartTesting = ref(false)
const autoStartTestMessage = ref('')
const autoStartTestOK = ref(false)
const gameStatus = ref<GameStatus | null>(null)
const settingsDraft = ref<Settings>({ ...settings.value })
const autoStartDraft = ref(false)

// --- Voice ---
const voiceStatus = ref<VoiceStatus | null>(null)
const voiceDevices = ref<VoiceDevice[]>([])
const voiceState = ref('stopped')
const voiceLevel = ref(0)
const voiceHeard = ref('')
const voiceError = ref('')
const voiceBusy = ref(false)
const micDiagnostics = ref<MicrophoneDiagnostics | null>(null)
const micChecking = ref(false)
const appScanning = ref(false)
const catalogStatus = ref<CatalogStatus>({ count: 0, updated_at: 0 })
const showLocationPrompt = ref(false)
const locationCity = ref('')
const locationBusy = ref(false)
const locationError = ref('')
const voiceLevelWidth = computed(() => Math.max(2, Math.min(100, Math.round(voiceLevel.value * 800))) + '%')
const voiceStateLabel = computed(() => {
  switch (voiceState.value) {
    case 'wake': return settings.value.wake_word_disabled ? 'Push-to-talk ready' : 'Waiting for wake word'
    case 'command': return 'Listening for command'
    case 'processing': return 'Recognizing'
    case 'speaking': return 'Speaking'
    case 'starting': return 'Starting'
    case 'error': return 'Error'
    case 'stopped': return 'Stopped'
    default: return voiceState.value
  }
})

// --- Screen analysis consent ---
const showScreenConfirm = ref(false)
const screenQuestion = ref('')

// --- Setup Wizard ---
const showSetup = ref(false)
const setupStep = ref(0) // 0/1=detecting, 2=overview, 3=running, 4=ready, 5=error
const setupPlan = ref<SetupPlan | null>(null)
const setupInfo = ref<SetupInfo | null>(null)
const setupModel = ref<ModelRecommendation | null>(null)
const downloadURL = ref('')
const setupProgress = ref<SetupProgress>({ stage: 'detect', message: '', percent: 0 })
const setupError = ref('')
const setupRunning = ref(false)

function addMessage(role: 'user' | 'assistant', content: string): Message {
  const message: Message = {
    id: msgId++, role, content, time: now()
  }
  messages.value.push(message)
  scrollToBottom(true)
  return message
}

function addImageMessage(src: string, size: string) {
  messages.value.push({ id: msgId++, role: 'assistant', content: '', time: now(), image: { src, size } })
  scrollToBottom(true)
}

function isNearBottom(): boolean {
  const el = chatRef.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 80
}

function scrollToBottom(force = false) {
  if (!force && !isNearBottom()) return
  nextTick(() => { if (chatRef.value) chatRef.value.scrollTop = chatRef.value.scrollHeight })
}

function finishActiveStream(requestID: string) {
  pendingStreamEvents.delete(requestID)
  if (activeStreamId.value !== requestID) return
  activeStreamId.value = ''
  loading.value = false
  scrollToBottom()
}

function clearChatUI() {
  messages.value = []
  activeStreamId.value = ''
  pendingStreamEvents.clear()
  loading.value = false
}

function applyStreamEvent(type: StreamEventType, event: ChatStreamEvent) {
  const message = messages.value.find(item => item.streamId === event.request_id)
  if (!message) {
    // A terminal event must always release the composer even if the target
    // message was replaced or removed while the request was in flight.
    if (type !== 'delta') finishActiveStream(event.request_id)
    return
  }

  if (type === 'delta') {
    if (!message.streaming) return
    message.content += event.delta || ''
    scrollToBottom()
    return
  }

  if (type === 'done') {
    if (event.content !== undefined) message.content = event.content
    message.streaming = false
    finishActiveStream(event.request_id)
    return
  }

  if (type === 'error') {
    const detail = event.error || 'The stream failed, Sir.'
    message.content += (message.content ? '\n\n' : '') + detail
    message.streaming = false
    finishActiveStream(event.request_id)
    return
  }

  message.content += (message.content ? '\n\n' : '') + '*Stopped.*'
  message.streaming = false
  finishActiveStream(event.request_id)
}

function onStreamEvent(type: StreamEventType, event: ChatStreamEvent) {
  const requestID = event?.request_id
  if (!requestID) return

  if (requestID === activeStreamId.value) {
    applyStreamEvent(type, event)
    return
  }

  // The stream can emit its first chunk before ChatStream returns the ID.
  if (!activeStreamId.value) {
    const events = pendingStreamEvents.get(requestID) || []
    events.push({ type, event })
    pendingStreamEvents.set(requestID, events)
  }
}

function attachStream(start: ChatStreamStart) {
  const message = addMessage('assistant', '')
  message.streaming = true
  message.streamId = start.request_id
  activeStreamId.value = start.request_id

  const events = pendingStreamEvents.get(start.request_id) || []
  pendingStreamEvents.delete(start.request_id)
  for (const item of events) {
    applyStreamEvent(item.type, item.event)
  }
}

function registerStreamListeners() {
  const runtime = window.runtime
  if (!runtime) return
  streamUnsubscribers.push(
    runtime.EventsOn('chat:delta', (payload: any) => onStreamEvent('delta', payload as ChatStreamEvent)),
    runtime.EventsOn('chat:done', (payload: any) => onStreamEvent('done', payload as ChatStreamEvent)),
    runtime.EventsOn('chat:error', (payload: any) => onStreamEvent('error', payload as ChatStreamEvent)),
    runtime.EventsOn('chat:canceled', (payload: any) => onStreamEvent('canceled', payload as ChatStreamEvent))
  )
}

function registerVoiceListeners() {
  const runtime = window.runtime
  if (!runtime) return
  streamUnsubscribers.push(
    runtime.EventsOn('voice:state', (payload: any) => {
      if (payload?.state) {
        voiceState.value = String(payload.state)
        if (voiceStatus.value) voiceStatus.value.input.state = String(payload.state)
      }
    }),
    runtime.EventsOn('voice:level', (payload: any) => {
      const level = Number(payload?.level || 0)
      voiceLevel.value = Number.isFinite(level) ? level : 0
    }),
    runtime.EventsOn('voice:transcript', (payload: any) => {
      const text = String(payload?.text || '').trim()
      if (!text) return
      voiceHeard.value = text
      addMessage('user', text)
      historyReady.value = true
    }),
    runtime.EventsOn('voice:message', (payload: any) => {
      const content = String(payload?.content || '').trim()
      if (!content) return
      addMessage('assistant', content)
    }),
    runtime.EventsOn('voice:heard', (payload: any) => {
      voiceHeard.value = String(payload?.text || '')
    }),
    runtime.EventsOn('voice:wake', () => {
      voiceState.value = 'command'
    }),
    runtime.EventsOn('voice:error', (payload: any) => {
      voiceError.value = String(payload?.message || payload || '')
    }),
    runtime.EventsOn('onboarding:tip', (payload: any) => {
      showOnboardingTip(String(payload?.content || ''))
    }),
    runtime.EventsOn('proactive:message', (payload: any) => {
      const content = String(payload?.content || '').trim()
      if (!content) return
      addMessage('assistant', content)
      if (payload?.kind === 'game' || payload?.kind === 'game_break') void loadGameStatus()
    })
  )
}

async function stopStreaming() {
  const requestID = activeStreamId.value
  if (!requestID) return
  try {
    await window.go.main.App.CancelChatStream(requestID)
  } catch (error) {
    addMessage('assistant', 'Could not stop the response: ' + errorText(error))
  }
}

async function showImageResponse(response: string) {
  if (!response.startsWith('IMAGE_SAVED:')) {
    addMessage('assistant', response || '...')
    return
  }
  const parts = response.split(':')
  const path = parts.slice(1, -1).join(':')
  const size = parts[parts.length - 1]
  try {
    const dataUrl = await window.go.main.App.LoadImageBase64(path)
    if (!dataUrl) {
      addMessage('assistant', 'The image was generated but could not be loaded.')
      return
    }
    addImageMessage(dataUrl, size)
  } catch (error) {
    addMessage('assistant', 'Error loading image: ' + errorText(error))
  }
}

async function sendMessage() {
  const text = input.value.trim()
  if (!historyReady.value || !text || loading.value) return
  addMessage('user', text)
  input.value = ''
  loading.value = true
  try {
    const start = await window.go.main.App.ChatStream(text)
    if (start.streaming) {
      attachStream(start)
      return
    }
    if (start.clear_history) {
      clearChatUI()
      return
    }
    await showImageResponse(start.content || '...')
    loading.value = false
  } catch (error) {
    addMessage('assistant', 'Error: ' + errorText(error))
    loading.value = false
  }
}

async function generateImage() {
  const prompt = imagePrompt.value.trim()
  if (!historyReady.value || !prompt || imageLoading.value) return
  imageLoading.value = true
  showImageModal.value = false
  addMessage('user', 'Generate: ' + prompt)
  try {
    const response = await window.go.main.App.GenerateImage(prompt, imageSize.value)
    await showImageResponse(response)
  } catch (error) {
    addMessage('assistant', 'Error: ' + errorText(error))
  } finally {
    imageLoading.value = false
  }
}

function requestScreenAnalysis() {
  if (!historyReady.value || loading.value) return
  screenQuestion.value = input.value.trim() || 'What is on my screen?'
  playVoice('screenshot_confirm')
  showScreenConfirm.value = true
}

function closeScreenConfirm() {
  playVoice('as_you_wish')
  showScreenConfirm.value = false
}

async function confirmScreenAnalysis() {
  if (loading.value) return
  const text = screenQuestion.value
  showScreenConfirm.value = false
  addMessage('user', '[Screen] ' + text)
  input.value = ''
  loading.value = true
  try {
    const response = await window.go.main.App.ProcessWithScreen(text)
    addMessage('assistant', response || '...')
  } catch (error) {
    addMessage('assistant', 'Error: ' + errorText(error))
  } finally {
    loading.value = false
  }
}

function quickCommand(cmd: string) {
  input.value = cmd
  nextTick(() => { const ta = document.querySelector('textarea'); if (ta) ta.focus() })
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    sendMessage()
  }
}

function visibleModal(): HTMLElement | null {
  const overlays = Array.from(document.querySelectorAll<HTMLElement>('.modal-overlay'))
  return overlays.length ? overlays[overlays.length - 1] : null
}

function focusFirstInModal() {
  const overlay = visibleModal()
  if (!overlay) return
  const target = overlay.querySelector<HTMLElement>(
    'input:not([disabled]), textarea:not([disabled]), select:not([disabled]), button:not([disabled])'
  )
  target?.focus()
}

function trapModalFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const overlay = visibleModal()
  if (!overlay) return
  const focusable = Array.from(
    overlay.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    )
  ).filter((element) => element.getClientRects().length > 0)
  if (focusable.length === 0) {
    event.preventDefault()
    return
  }
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement
  if (event.shiftKey && (active === first || !overlay.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (active === last || !overlay.contains(active))) {
    event.preventDefault()
    first.focus()
  }
}

function closeTopModal() {
  if (showScreenConfirm.value) {
    closeScreenConfirm()
  } else if (showImageModal.value) {
    closeImageModal()
  } else if (showSettings.value) {
    closeSettings()
  } else if (showLocationPrompt.value) {
    dismissLocationPrompt()
  }
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    const overlay = visibleModal()
    if (!overlay || showSetup.value) return
    event.preventDefault()
    closeTopModal()
    return
  }
  trapModalFocus(event)
}

async function openExternal(url: string) {
  try {
    await window.go.main.App.OpenExternal(url)
  } catch (error) {
    addMessage('assistant', 'Could not open link: ' + errorText(error))
  }
}

function handleMessageClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null
  const link = target?.closest('a')
  if (!link) return
  const href = link.getAttribute('href')
  if (!href) return
  event.preventDefault()
  openExternal(href)
}

// --- Setup Wizard ---
function applySetupPlan(plan: SetupPlan) {
  setupPlan.value = plan
  setupInfo.value = plan.info
  setupModel.value = plan.recommendation
}

function progressWidth(): string {
  return Math.max(2, Math.min(100, setupProgress.value.percent)) + '%'
}

async function startSetup(preloaded?: SetupPlan) {
  if (setupRunning.value) return
  showSetup.value = true
  setupStep.value = 1
  setupError.value = ''
  setupPlan.value = null
  setupProgress.value = { stage: 'detect', message: 'Scanning hardware and installed models...', percent: 0 }
  try {
    const plan = preloaded || await window.go.main.App.GetSetupPlan()
    applySetupPlan(plan)
    if (!downloadURL.value) {
      downloadURL.value = await window.go.main.App.GetOllamaDownloadURL()
    }
    setupStep.value = plan.ready ? 4 : 2
  } catch (error) {
    setupStep.value = 5
    setupError.value = 'Detection failed: ' + errorText(error)
  }
}

async function runAutomaticSetup() {
  if (setupRunning.value) return
  if (!setupPlan.value) {
    await startSetup()
    if (!setupPlan.value) return
  }

  setupRunning.value = true
  setupStep.value = 3
  setupError.value = ''
  setupProgress.value = { stage: 'detect', message: 'Preparing automatic setup...', percent: 0 }
  try {
    const model = setupPlan.value.selected_model || setupPlan.value.recommendation.model
    const result = await window.go.main.App.RunSetup(model)
    setupProgress.value = { stage: 'done', message: result, percent: 100 }
    await loadSettings()
    try {
      applySetupPlan(await window.go.main.App.GetSetupPlan())
    } catch {
      // The setup itself succeeded; the next refresh can update the summary.
    }
    setupStep.value = 4
  } catch (error) {
    setupError.value = errorText(error)
    setupStep.value = 5
  } finally {
    setupRunning.value = false
  }
}

async function retrySetup() {
  await startSetup()
  if (setupPlan.value && !setupPlan.value.ready) {
    await runAutomaticSetup()
  }
}

function setupWasDismissed(): boolean {
  try {
    return localStorage.getItem('jarvis.setupDismissed') === '1'
  } catch {
    return false
  }
}

function markSetupDismissed() {
  try {
    localStorage.setItem('jarvis.setupDismissed', '1')
  } catch {
    // A locked-down WebView may not allow storage; skipping still works.
  }
}

async function finishSetup() {
  showSetup.value = false
  markSetupDismissed()
  const plan = setupPlan.value
  if (plan?.ready && plan.selected_model) {
    settings.value.model = plan.selected_model
    try {
      await window.go.main.App.SaveSettings(settings.value)
    } catch (error) {
      addMessage('assistant', 'Could not save the model selection: ' + errorText(error))
    }
  }
  await loadSettings()
}

async function maybeOpenSetup() {
  if (showSetup.value) return
  try {
    const plan = await window.go.main.App.GetSetupPlan()
    applySetupPlan(plan)
    if (plan.ready) return
    // A missing Ollama installation or a missing active model makes the
    // assistant unusable, so open the wizard even after a previous skip.
    if (!setupWasDismissed() || plan.needs_install || !plan.current_model_installed) {
      await startSetup(plan)
    }
  } catch {
    if (!setupWasDismissed()) {
      await startSetup()
    }
  }
}

function registerSetupListener() {
  const runtime = window.runtime
  if (!runtime) return
  streamUnsubscribers.push(
    runtime.EventsOn('setup:progress', (payload: any) => {
      const progress = payload as SetupProgress
      if (!progress) return
      setupProgress.value = progress
      if (progress.stage === 'error') {
        setupError.value = progress.message
      }
    })
  )
}

async function loadSettings() {
  try {
    settings.value = await window.go.main.App.GetSettings()
    if (!showSettings.value) settingsDraft.value = { ...settings.value }
    status.value = await window.go.main.App.GetSystemStatus()
    availableModels.value = await window.go.main.App.GetAvailableModels()
    autoStart.value = await window.go.main.App.IsAutoStart()
    await loadVoiceStatus()
    locationCity.value = settings.value.voice_city || ''
    if (!settings.value.voice_location_asked && !settings.value.voice_city) {
      showLocationPrompt.value = true
    } else {
      showLocationPrompt.value = false
    }
  } catch (error) {
    settingsError.value = 'Could not load settings: ' + errorText(error)
  }
}

async function loadVoiceStatus() {
  try {
    const snapshot = await window.go.main.App.GetVoiceStatus()
    voiceStatus.value = snapshot
    voiceState.value = snapshot.input?.state || snapshot.state || 'stopped'
  } catch (error) {
    voiceError.value = errorText(error)
  }
}

async function loadVoiceDevices() {
  try {
    voiceDevices.value = await window.go.main.App.GetVoiceDevices()
  } catch (error) {
    voiceError.value = 'Could not list microphones: ' + errorText(error)
  }
}

// Location changes are persisted by the backend together with their
// coordinates. Refresh the saved snapshot so a later SaveSettings call cannot
// overwrite the coordinates with the stale values from the draft.
async function refreshLocationSettings() {
  try {
    const fresh = await window.go.main.App.GetSettings()
    settings.value = fresh
    Object.assign(settingsDraft.value, {
      voice_city: fresh.voice_city,
      voice_latitude: fresh.voice_latitude,
      voice_longitude: fresh.voice_longitude,
      voice_location_asked: fresh.voice_location_asked
    })
    locationCity.value = fresh.voice_city || locationCity.value
  } catch (error) {
    voiceError.value = 'Could not refresh location settings: ' + errorText(error)
  }
}

async function startVoice(): Promise<boolean> {
  if (voiceBusy.value) return false
  voiceBusy.value = true
  voiceError.value = ''
  try {
    await window.go.main.App.StartVoiceInput()
    await loadVoiceStatus()
    return true
  } catch (error) {
    voiceError.value = errorText(error)
    return false
  } finally {
    voiceBusy.value = false
  }
}

async function stopVoice(): Promise<boolean> {
  if (voiceBusy.value) return false
  voiceBusy.value = true
  voiceError.value = ''
  try {
    await window.go.main.App.StopVoiceInput()
    await loadVoiceStatus()
    return true
  } catch (error) {
    voiceError.value = errorText(error)
    return false
  } finally {
    voiceBusy.value = false
  }
}

async function toggleVoiceInput() {
  if (voiceBusy.value) return
  const previous = settingsDraft.value.voice_input_disabled
  const next = !previous
  settingsDraft.value.voice_input_disabled = next
  settings.value.voice_input_disabled = next
  const ok = next ? await stopVoice() : await startVoice()
  if (!ok) {
    settingsDraft.value.voice_input_disabled = previous
    settings.value.voice_input_disabled = previous
    await loadVoiceStatus()
  }
}

async function checkMicrophone() {
  if (micChecking.value) return
  micChecking.value = true
  voiceError.value = ''
  try {
    micDiagnostics.value = await window.go.main.App.GetMicrophoneDiagnostics()
  } catch (error) {
    voiceError.value = errorText(error)
  } finally {
    micChecking.value = false
  }
}

async function detectVoiceLocation() {
  voiceError.value = ''
  try {
    const city = await window.go.main.App.DetectWeatherLocation()
    settingsDraft.value.voice_city = city
    settingsDraft.value.voice_location_asked = true
    settings.value.voice_city = city
    settings.value.voice_location_asked = true
    await refreshLocationSettings()
    await loadVoiceStatus()
  } catch (error) {
    voiceError.value = 'Could not detect location: ' + errorText(error)
  }
}

async function applyVoiceCity() {
  const city = settingsDraft.value.voice_city.trim()
  if (!city) return
  voiceError.value = ''
  try {
    const canonical = await window.go.main.App.SetWeatherCity(city)
    settingsDraft.value.voice_city = canonical
    settingsDraft.value.voice_location_asked = true
    settings.value.voice_city = canonical
    settings.value.voice_location_asked = true
    await refreshLocationSettings()
    await loadVoiceStatus()
  } catch (error) {
    voiceError.value = 'Could not set city: ' + errorText(error)
  }
}

async function useIpLocation() {
  if (locationBusy.value) return
  const wasOpen = showLocationPrompt.value
  locationBusy.value = true
  locationError.value = ''
  try {
    const city = await window.go.main.App.DetectWeatherLocation()
    settings.value.voice_city = city
    settings.value.voice_location_asked = true
    settingsDraft.value.voice_city = city
    settingsDraft.value.voice_location_asked = true
    locationCity.value = city
    showLocationPrompt.value = false
    await refreshLocationSettings()
    await loadVoiceStatus()
  } catch (error) {
    locationError.value = errorText(error)
    if (wasOpen) showLocationPrompt.value = true
  } finally {
    locationBusy.value = false
  }
}

async function saveLocationCity() {
  const city = locationCity.value.trim()
  if (!city || locationBusy.value) return
  const wasOpen = showLocationPrompt.value
  locationBusy.value = true
  locationError.value = ''
  try {
    const canonical = await window.go.main.App.SetWeatherCity(city)
    settings.value.voice_city = canonical
    settings.value.voice_location_asked = true
    settingsDraft.value.voice_city = canonical
    settingsDraft.value.voice_location_asked = true
    locationCity.value = canonical
    showLocationPrompt.value = false
    await refreshLocationSettings()
    await loadVoiceStatus()
  } catch (error) {
    locationError.value = errorText(error)
    if (wasOpen) showLocationPrompt.value = true
  } finally {
    locationBusy.value = false
  }
}

async function dismissLocationPrompt() {
  const wasOpen = showLocationPrompt.value
  showLocationPrompt.value = false
  try {
    await window.go.main.App.DismissVoiceLocation()
    settings.value.voice_location_asked = true
    settingsDraft.value.voice_location_asked = true
  } catch (error) {
    locationError.value = errorText(error)
    if (wasOpen) showLocationPrompt.value = true
  }
}

async function selectVoiceDevice() {
  const previousID = settings.value.voice_device_id
  const previousName = settings.value.voice_device_name
  const nextID = settingsDraft.value.voice_device_id
  const device = voiceDevices.value.find((item) => item.id === nextID)
  const nextName = device?.name || ''
  settingsDraft.value.voice_device_name = nextName
  settings.value.voice_device_id = nextID
  settings.value.voice_device_name = nextName
  voiceError.value = ''
  try {
    await window.go.main.App.SetVoiceDevice(nextID, nextName)
    await loadVoiceStatus()
  } catch (error) {
    settingsDraft.value.voice_device_id = previousID
    settingsDraft.value.voice_device_name = previousName
    settings.value.voice_device_id = previousID
    settings.value.voice_device_name = previousName
    voiceError.value = 'Could not switch microphone: ' + errorText(error)
    await loadVoiceStatus()
  }
}

async function pushToTalk() {
  try {
    const started = await window.go.main.App.PushToTalk()
    if (!started) {
      voiceError.value = 'Voice input is not ready. Start it in Settings.'
    }
  } catch (error) {
    voiceError.value = errorText(error)
  }
}

async function openSettings() {
  playVoice('at_your_service')
  settingsDraft.value = { ...settings.value }
  autoStartDraft.value = autoStart.value
  settingsError.value = ''
  showSettings.value = true
  autoStartTestMessage.value = ''
  autoStartTestOK.value = false
  try {
    status.value = await window.go.main.App.GetSystemStatus()
    await loadAutoStartStatus()
    await loadVoiceStatus()
    await loadVoiceDevices()
    void loadGameStatus()
    if (!micDiagnostics.value) void checkMicrophone()
  } catch {
    // Keep the last known status if the health check fails.
  }
}

function closeSettings() {
  playVoice('as_you_wish')
  showSettings.value = false
}

function openImageModal() {
  playVoice('at_your_service')
  showImageModal.value = true
}

function closeImageModal() {
  playVoice('as_you_wish')
  showImageModal.value = false
}

function testVoice() {
  playVoice('at_your_service')
}

async function saveSettings() {
  settingsError.value = ''
  try {
    const candidate = { ...settingsDraft.value }
    await window.go.main.App.SaveSettings(candidate)
    const saved = await window.go.main.App.GetSettings()
    settings.value = saved
    settingsDraft.value = { ...saved }
    if (!(await window.go.main.App.SetAutoStart(autoStartDraft.value))) {
      settingsError.value = 'Settings saved, but auto-start could not be changed.'
      return
    }
    autoStart.value = autoStartDraft.value
    await loadAutoStartStatus()
    showSettings.value = false
    status.value = await window.go.main.App.GetSystemStatus()
    availableModels.value = await window.go.main.App.GetAvailableModels()
    await loadVoiceStatus()
  } catch (error) {
    settingsError.value = errorText(error)
  }
}

async function refreshModels() {
  try {
    availableModels.value = await window.go.main.App.GetAvailableModels()
    status.value = await window.go.main.App.GetSystemStatus()
    playVoice('check_done')
  } catch (error) {
    playVoice('error')
    settingsError.value = 'Could not refresh models: ' + errorText(error)
  }
}

async function loadApps() {
  try {
    installedApps.value = await window.go.main.App.GetInstalledApps()
    catalogStatus.value = await window.go.main.App.GetCatalogStatus()
  } catch (error) {
    addMessage('assistant', 'Could not detect installed applications: ' + errorText(error))
  }
}

async function scanApps() {
  if (appScanning.value) return
  appScanning.value = true
  try {
    const count = await window.go.main.App.ScanApplications()
    catalogStatus.value = { count, updated_at: Date.now() }
    playVoice('check_done')
  } catch (error) {
    playVoice('error')
    addMessage('assistant', 'Application scan failed: ' + errorText(error))
  } finally {
    appScanning.value = false
  }
}

function catalogUpdatedLabel(): string {
  if (!catalogStatus.value.updated_at) return 'not scanned yet'
  return new Date(catalogStatus.value.updated_at).toLocaleString()
}

async function loadAutoStartStatus() {
  try {
    autoStartStatus.value = await window.go.main.App.GetAutoStartStatus()
    autoStart.value = autoStartStatus.value.enabled
  } catch {
    // Diagnostics are optional; the toggle still works without them.
  }
}

async function testAutoStart() {
  if (autoStartTesting.value) return
  autoStartTesting.value = true
  autoStartTestMessage.value = ''
  try {
    const [ok, message] = await window.go.main.App.TestAutoStart()
    autoStartTestOK.value = ok
    autoStartTestMessage.value = message
    playVoice(ok ? 'check_done' : 'error')
  } catch (error) {
    autoStartTestOK.value = false
    autoStartTestMessage.value = errorText(error)
    playVoice('error')
  } finally {
    autoStartTesting.value = false
  }
}

async function loadGameStatus() {
  try {
    gameStatus.value = await window.go.main.App.GetGameStatus()
  } catch {
    // Game tracking is optional.
  }
}

function gameDuration(seconds: number): string {
  if (!seconds || seconds < 60) return 'less than a minute'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return minutes + ' min'
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest ? hours + ' h ' + rest + ' min' : hours + ' h'
}

function showOnboardingTip(content: string) {
  const text = content.trim()
  if (!text) return
  addMessage('assistant', text)
  // Keep the local snapshot in sync so a later SaveSettings does not look
  // like it is re-arming the tip.
  settings.value.onboarding_shown = true
  settingsDraft.value.onboarding_shown = true
}

async function loadOnboardingTip() {
  try {
    showOnboardingTip(await window.go.main.App.GetOnboardingTip())
  } catch {
    // The tip is cosmetic; a failure must not block startup.
  }
}

async function loadChatHistory() {
  try {
    const history = await window.go.main.App.GetHistory()
    if (!history.length) return

    messages.value = history.map((message) => ({
      id: msgId++,
      role: message.role,
      content: message.content,
      time: ''
    }))
    nextTick(() => scrollToBottom(true))
  } catch (error) {
    console.error('Could not load chat history:', error)
  } finally {
    historyReady.value = true
  }
}

async function launchApp(app: InstalledApp) {
  try {
    const result = await window.go.main.App.LaunchApp(app.name)
    addMessage('assistant', result)
  } catch (error) {
    addMessage('assistant', 'Error: ' + errorText(error))
  }
}

let lastFocused: HTMLElement | null = null
watch([showSettings, showImageModal, showScreenConfirm, showLocationPrompt, showSetup], (states) => {
  const anyOpen = states.some(Boolean)
  if (anyOpen) {
    if (!lastFocused) lastFocused = document.activeElement as HTMLElement | null
    void nextTick(focusFirstInModal)
  } else if (lastFocused) {
    lastFocused.focus()
    lastFocused = null
  }
})

onMounted(async () => {
  window.addEventListener('keydown', handleGlobalKeydown)
  registerStreamListeners()
  registerVoiceListeners()
  registerSetupListener()
  await loadChatHistory()
  await loadSettings()
  loadVoiceDevices()
  loadApps()
  void loadOnboardingTip()
  void maybeOpenSetup()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  const requestID = activeStreamId.value
  if (requestID) {
    void window.go.main.App.CancelChatStream(requestID).catch(() => {})
  }
  for (const unsubscribe of streamUnsubscribers) unsubscribe()
  streamUnsubscribers = []
})
</script>

<template>
  <div class="h-screen w-screen flex flex-col overflow-hidden" style="background:#0a0e1a">
    <!-- Header -->
    <header class="glass-strong flex items-center justify-between px-5 py-3 flex-shrink-0" style="border-bottom:1px solid rgba(0,212,255,0.08)">
      <div class="flex items-center gap-4">
        <button type="button" @click="showSidebar = !showSidebar" class="text-gray-500 hover:text-white transition-colors p-1" title="Toggle sidebar" aria-label="Toggle sidebar">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M3 12h18M3 6h18M3 18h18"/>
          </svg>
        </button>
        <div class="flex items-center gap-2.5">
          <svg viewBox="0 0 100 100" width="30" height="30" style="filter:drop-shadow(0 0 6px rgba(0,212,255,0.4))">
            <circle cx="50" cy="50" r="48" fill="none" stroke="rgba(0,212,255,0.1)" stroke-width="1"/>
            <circle cx="50" cy="50" r="44" fill="none" stroke="#00d4ff" stroke-width="2" stroke-dasharray="4 6" opacity="0.5"/>
            <circle cx="50" cy="50" r="38" fill="none" stroke="#00bfff" stroke-width="2.5"/>
            <circle cx="50" cy="50" r="30" fill="none" stroke="#0066ff" stroke-width="1.5" opacity="0.4"/>
            <circle cx="50" cy="50" r="4" fill="#00d4ff" opacity="0.9"/>
            <line x1="50" y1="12" x2="50" y2="22" stroke="#00d4ff" stroke-width="1.5" opacity="0.5"/>
            <line x1="50" y1="78" x2="50" y2="88" stroke="#00d4ff" stroke-width="1.5" opacity="0.5"/>
            <line x1="12" y1="50" x2="22" y2="50" stroke="#00d4ff" stroke-width="1.5" opacity="0.5"/>
            <line x1="78" y1="50" x2="88" y2="50" stroke="#00d4ff" stroke-width="1.5" opacity="0.5"/>
            <text x="50" y="58" text-anchor="middle" fill="#00d4ff" font-family="'Segoe UI',sans-serif" font-size="36" font-weight="700">J</text>
          </svg>
          <span class="font-bold text-sm tracking-widest" style="background:linear-gradient(135deg,#00d4ff,#0066ff);-webkit-background-clip:text;-webkit-text-fill-color:transparent">JARVIS</span>
        </div>
      </div>
      <div class="flex items-center gap-4">
        <div class="flex items-center gap-2">
          <span class="status-dot" :class="status.ollama_running ? 'online' : 'offline'"></span>
          <span class="text-xs text-gray-500">{{ status.model || 'Offline' }}</span>
        </div>
        <button type="button" @click="openSettings" class="text-gray-500 hover:text-white transition-colors p-1" title="Settings" aria-label="Open settings">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>
          </svg>
        </button>
      </div>
    </header>

    <div class="flex flex-1 overflow-hidden">
      <!-- Sidebar -->
      <aside v-show="showSidebar" class="glass flex-shrink-0 flex flex-col p-3 gap-1 overflow-y-auto"
        style="width:200px;border-right:1px solid rgba(0,212,255,0.08)">
        <div class="flex items-center justify-between px-3 py-2">
          <span class="text-xs text-gray-600 uppercase tracking-wider font-semibold">System Tools</span>
        </div>
        <button class="sidebar-btn" v-for="app in installedApps" :key="app.name" @click="launchApp(app)">
          <span v-if="app.icon === 'chrome' || app.icon === 'edge' || app.icon === 'firefox' || app.icon === 'brave' || app.icon === 'opera' || app.icon === 'yandex'" class="text-cyan-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/><line x1="21.17" y1="8" x2="12" y2="8"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'cmd'" class="text-gray-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'powershell'" class="text-blue-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M18 10h-1.5A1.5 1.5 0 0015 11.5v1a1.5 1.5 0 001.5 1.5H18"/><path d="M6 8l4 4-4 4"/><rect x="2" y="4" width="20" height="16" rx="2"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'terminal'" class="text-cyan-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'vscode'" class="text-blue-500">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M16 18l6-6-6-6"/><path d="M8 6l-6 6 6 6"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'notepad'" class="text-yellow-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'explorer'" class="text-yellow-500">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'spotify'" class="text-green-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/><path d="M8 14.5c2.5-1 5.5-1 8 0"/><path d="M9.5 11.5c2-1 4.5-1 6.5 0"/><path d="M11 8.5c1.5-.5 3-.5 4.5 0"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'discord'" class="text-purple-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 12h6"/><path d="M12 9v6"/><circle cx="12" cy="12" r="10"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'telegram'" class="text-blue-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/>
            </svg>
          </span>
          <span v-else-if="app.icon === 'calc'" class="text-gray-400">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="4" y="2" width="16" height="20" rx="2"/><line x1="8" y1="6" x2="16" y2="6"/>
            </svg>
          </span>
          <span v-else class="text-gray-500">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
            </svg>
          </span>
          {{ app.name }}
        </button>
        <div class="mt-auto pt-3 border-t border-gray-800">
          <div class="text-xs text-gray-600 px-3">
            <div class="flex items-center gap-1.5 mb-1">
              <span class="status-dot" :class="status.ollama_running ? 'online' : 'offline'" style="width:6px;height:6px"></span>
              <span>{{ status.provider || 'Disconnected' }}</span>
            </div>
            <div>{{ status.model }}</div>
          </div>
        </div>
      </aside>

      <!-- Chat Area -->
      <main class="flex-1 flex flex-col overflow-hidden">
        <div ref="chatRef" class="flex-1 overflow-y-auto px-6 py-4">
          <!-- Landing Screen -->
          <div v-if="showLanding" class="h-full flex flex-col items-center justify-center">
            <svg viewBox="0 0 100 100" width="80" height="80" style="filter:drop-shadow(0 0 20px rgba(0,212,255,0.4));animation:pulse-glow 3s infinite">
              <circle cx="50" cy="50" r="48" fill="none" stroke="rgba(0,212,255,0.1)" stroke-width="1"/>
              <circle cx="50" cy="50" r="44" fill="none" stroke="#00d4ff" stroke-width="2" stroke-dasharray="4 6" opacity="0.5"/>
              <circle cx="50" cy="50" r="38" fill="none" stroke="#00bfff" stroke-width="2.5"/>
              <circle cx="50" cy="50" r="30" fill="none" stroke="#0066ff" stroke-width="1.5" opacity="0.4"/>
              <circle cx="50" cy="50" r="4" fill="#00d4ff" opacity="0.9"/>
              <line x1="50" y1="10" x2="50" y2="20" stroke="#00d4ff" stroke-width="2" opacity="0.5"/>
              <line x1="50" y1="80" x2="50" y2="90" stroke="#00d4ff" stroke-width="2" opacity="0.5"/>
              <line x1="10" y1="50" x2="20" y2="50" stroke="#00d4ff" stroke-width="2" opacity="0.5"/>
              <line x1="80" y1="50" x2="90" y2="50" stroke="#00d4ff" stroke-width="2" opacity="0.5"/>
              <text x="50" y="60" text-anchor="middle" fill="#00d4ff" font-family="'Segoe UI',sans-serif" font-size="40" font-weight="700">J</text>
            </svg>
            <h1 class="text-2xl font-bold mt-5 tracking-widest" style="background:linear-gradient(135deg,#00d4ff,#0066ff);-webkit-background-clip:text;-webkit-text-fill-color:transparent">HELLO, SIR</h1>
            <p class="text-xs text-gray-600 mt-1 tracking-wider uppercase">How may I assist you today?</p>
            <div class="grid grid-cols-2 gap-3 mt-8">
              <button @click="openImageModal" class="glass rounded-xl px-5 py-3 text-sm text-gray-400 hover:text-white hover:border-cyan-500/40 transition-all flex items-center gap-2.5" style="border:1px solid rgba(0,212,255,0.1);min-width:140px">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="M21 15l-5-5L5 21"/>
                </svg>
                Generate Image
              </button>
              <button @click="requestScreenAnalysis" class="glass rounded-xl px-5 py-3 text-sm text-gray-400 hover:text-white hover:border-cyan-500/40 transition-all flex items-center gap-2.5" style="border:1px solid rgba(0,212,255,0.1);min-width:140px">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M23 19a2 2 0 01-2 2H3a2 2 0 01-2-2V8a2 2 0 012-2h4l2-3h6l2 3h4a2 2 0 012 2z"/><circle cx="12" cy="13" r="4"/>
                </svg>
                Screen Analysis
              </button>
              <button @click="quickCommand('/smart ')" class="glass rounded-xl px-5 py-3 text-sm text-gray-400 hover:text-white hover:border-cyan-500/40 transition-all flex items-center gap-2.5" style="border:1px solid rgba(0,212,255,0.1);min-width:140px">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                  <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/>
                </svg>
                Smart Search
              </button>
              <button @click="quickCommand('/clear')" class="glass rounded-xl px-5 py-3 text-sm text-gray-400 hover:text-white hover:border-cyan-500/40 transition-all flex items-center gap-2.5" style="border:1px solid rgba(0,212,255,0.1);min-width:140px">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M3 6h18"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
                </svg>
                Clear Chat
              </button>
            </div>
          </div>
          <!-- Messages -->
          <div v-else class="space-y-4" @click="handleMessageClick">
            <div v-for="msg in messages" :key="msg.id" class="msg-enter flex items-start gap-3"
              :class="msg.role === 'user' ? 'flex-row-reverse' : ''">
              <div v-if="msg.role === 'assistant'" class="flex-shrink-0">
                <svg viewBox="0 0 100 100" width="30" height="30" style="filter:drop-shadow(0 0 4px rgba(0,212,255,0.3))">
                  <circle cx="50" cy="50" r="48" fill="none" stroke="rgba(0,212,255,0.08)" stroke-width="1"/>
                  <circle cx="50" cy="50" r="44" fill="none" stroke="#00d4ff" stroke-width="2" stroke-dasharray="4 6" opacity="0.4"/>
                  <circle cx="50" cy="50" r="38" fill="none" stroke="#00bfff" stroke-width="2.5"/>
                  <circle cx="50" cy="50" r="30" fill="none" stroke="#0066ff" stroke-width="1.5" opacity="0.3"/>
                  <circle cx="50" cy="50" r="3.5" fill="#00d4ff" opacity="0.8"/>
                  <text x="50" y="57" text-anchor="middle" fill="#00d4ff" font-family="'Segoe UI',sans-serif" font-size="34" font-weight="700">J</text>
                </svg>
              </div>
              <div v-else class="avatar-icon user">U</div>
              <div :class="['msg-bubble', msg.role]">
                <div v-if="msg.image" class="image-result">
                  <img :src="msg.image.src" :alt="'Generated image (' + msg.image.size + ')'" />
                  <div class="msg-time">{{ msg.image.size }}</div>
                </div>
                <div v-else-if="msg.streaming" class="msg-markdown msg-streaming">{{ msg.content }}<span class="stream-cursor" aria-hidden="true"></span></div>
                <div v-else class="msg-markdown" v-html="renderMarkdown(msg.content)"></div>
                <div v-if="!msg.image && msg.time" class="msg-time">{{ msg.time }}</div>
              </div>
            </div>
            <div v-if="loading && !activeStreamId" class="flex items-start gap-3">
              <svg viewBox="0 0 100 100" width="30" height="30" style="filter:drop-shadow(0 0 4px rgba(0,212,255,0.3))">
                <circle cx="50" cy="50" r="48" fill="none" stroke="rgba(0,212,255,0.08)" stroke-width="1"/>
                <circle cx="50" cy="50" r="44" fill="none" stroke="#00d4ff" stroke-width="2" stroke-dasharray="4 6" opacity="0.4"/>
                <circle cx="50" cy="50" r="38" fill="none" stroke="#00bfff" stroke-width="2.5"/>
                <circle cx="50" cy="50" r="30" fill="none" stroke="#0066ff" stroke-width="1.5" opacity="0.3"/>
                <circle cx="50" cy="50" r="3.5" fill="#00d4ff" opacity="0.8"/>
                <text x="50" y="57" text-anchor="middle" fill="#00d4ff" font-family="'Segoe UI',sans-serif" font-size="34" font-weight="700">J</text>
              </svg>
              <div class="msg-bubble assistant">
                <div class="typing-indicator flex gap-1.5 py-1">
                  <span></span><span></span><span></span>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="flex-shrink-0 px-4 py-3 glass-strong" style="border-top:1px solid rgba(0,212,255,0.08)">
          <div class="flex items-end gap-2 neon-border rounded-xl px-4 py-2">
            <textarea v-model="input" :disabled="!historyReady" placeholder="Type a message, Sir..."
              @keydown="handleKeydown" rows="1"
              class="flex-1 bg-transparent border-none outline-none text-sm text-white placeholder-gray-600 resize-none py-1.5 disabled:opacity-50"
              style="scrollbar-width:none"></textarea>
            <button type="button" @click="pushToTalk" :disabled="!historyReady" class="transition-colors p-1"
              :class="voiceState === 'command' ? 'text-cyan-400 animate-pulse' : 'text-gray-600 hover:text-cyan-400'"
              title="Push to talk" aria-label="Push to talk">
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2a3 3 0 00-3 3v7a3 3 0 006 0V5a3 3 0 00-3-3z"/>
                <path d="M19 10v2a7 7 0 01-14 0v-2"/><line x1="12" y1="19" x2="12" y2="22"/>
              </svg>
            </button>
            <button type="button" @click="requestScreenAnalysis" :disabled="loading || !historyReady" class="text-gray-600 hover:text-cyan-400 transition-colors p-1" title="Analyze screen" aria-label="Analyze screen">
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M23 19a2 2 0 01-2 2H3a2 2 0 01-2-2V8a2 2 0 012-2h4l2-3h6l2 3h4a2 2 0 012 2z"/><circle cx="12" cy="13" r="4"/>
              </svg>
            </button>
            <button v-if="activeStreamId" type="button" @click="stopStreaming"
              class="text-white rounded-lg p-1.5 transition-all flex-shrink-0 bg-red-600 hover:bg-red-500"
              title="Stop generating" aria-label="Stop generating">
              <svg viewBox="0 0 24 24" width="18" height="18">
                <rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor"/>
              </svg>
            </button>
            <button v-else type="button" :disabled="!historyReady || !input.trim() || loading" @click="sendMessage"
              class="text-white rounded-lg p-1.5 transition-all flex-shrink-0" aria-label="Send message"
              :class="historyReady && input.trim() && !loading ? 'bg-blue-600 hover:bg-blue-500' : 'bg-gray-800'">
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/>
              </svg>
            </button>
          </div>
        </div>
      </main>
    </div>

    <!-- Settings Modal -->
    <div class="modal-overlay" v-if="showSettings" @click.self="closeSettings">
      <div class="modal-panel glass-strong neon-glow" role="dialog" aria-modal="true" aria-label="Settings">
        <h2>SETTINGS</h2>
        <div class="space-y-4">
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Ollama URL</label>
            <input v-model="settingsDraft.ollama_url" class="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors" />
          </div>
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Model</label>
            <div class="flex gap-2">
              <select v-model="settingsDraft.model" class="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors">
                <option v-for="m in modelOptions" :key="m" :value="m">{{ m }}</option>
              </select>
              <button @click="refreshModels" class="btn-ghost text-xs px-3">Refresh</button>
            </div>
            <button @click="closeSettings(); startSetup()" class="btn-ghost w-full text-xs mt-2">RUN AUTOMATIC SETUP</button>
          </div>
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">System Prompt</label>
            <textarea v-model="settingsDraft.system_prompt" rows="6"
              class="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors resize-none font-mono"></textarea>
          </div>
          <div class="flex items-center justify-between">
            <div>
              <span class="text-sm text-gray-400">Voice phrases</span>
              <p class="text-xs text-gray-600 mt-0.5">JARVIS speaks while performing actions</p>
            </div>
            <div class="flex items-center gap-2">
              <button @click="testVoice" class="btn-ghost text-xs px-3">TEST</button>
              <button type="button" role="switch" :aria-checked="!settingsDraft.voice_disabled"
                aria-label="Voice phrases" @click="settingsDraft.voice_disabled = !settingsDraft.voice_disabled"
                class="w-10 h-5 rounded-full transition-colors relative"
                :class="!settingsDraft.voice_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="!settingsDraft.voice_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
          </div>
          <div class="border-t border-gray-800 pt-3 mt-1 space-y-3">
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm text-gray-400">Voice input &amp; wake word</span>
                <p class="text-xs text-gray-600 mt-0.5">Say «{{ settingsDraft.voice_wake_word || 'привет джарвис' }}»</p>
              </div>
              <button type="button" role="switch" :aria-checked="!settingsDraft.voice_input_disabled"
                aria-label="Voice input and wake word" @click="toggleVoiceInput" :disabled="voiceBusy"
                class="w-10 h-5 rounded-full transition-colors relative"
                :class="!settingsDraft.voice_input_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="!settingsDraft.voice_input_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
            <div class="flex items-center gap-2 text-xs text-gray-500">
              <span class="status-dot" :class="voiceState !== 'stopped' && voiceState !== 'error' ? 'online' : 'offline'" style="width:6px;height:6px"></span>
              <span>{{ voiceStateLabel }}</span>
              <div class="flex-1 h-1.5 bg-gray-800 rounded-full overflow-hidden">
                <div class="h-full bg-cyan-400 transition-all" :style="{ width: voiceLevelWidth }"></div>
              </div>
            </div>
            <p v-if="voiceStatus" class="text-xs text-gray-600">
              Wake word: Vosk {{ voiceStatus.engine.vosk_ready ? 'RU ready' : 'RU missing' }}
              {{ voiceStatus.engine.vosk_ready_en ? '· EN ready' : '' }} ·
              Whisper {{ voiceStatus.engine.whisper_ready ? voiceStatus.engine.whisper_model + ' (' + voiceStatus.engine.whisper_backend + ')' : 'missing' }}
            </p>
            <p v-if="voiceStatus" class="text-[10px] text-gray-700 font-mono break-all">{{ voiceStatus.engine.root }}</p>
            <p v-if="voiceStatus && voiceStatus.engine.integrity_error" class="text-xs text-red-400">
              Voice engine integrity check failed: {{ voiceStatus.engine.integrity_error }}
            </p>
            <div>
              <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Microphone</label>
              <div class="flex gap-2">
                <select v-model="settingsDraft.voice_device_id" @change="selectVoiceDevice"
                  class="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors">
                  <option value="">System default</option>
                  <option v-for="device in voiceDevices" :key="device.id" :value="device.id">
                    {{ device.name }}{{ device.is_default ? ' (default)' : '' }}
                  </option>
                </select>
                <button @click="pushToTalk" class="btn-ghost text-xs px-3" title="Push to talk">MIC</button>
              </div>
            </div>
            <div>
              <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Wake word</label>
              <div class="flex gap-2">
                <input v-model="settingsDraft.voice_wake_word"
                  class="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors" />
                <button type="button" role="switch" :aria-checked="!settingsDraft.wake_word_disabled"
                  aria-label="Wake word" @click="settingsDraft.wake_word_disabled = !settingsDraft.wake_word_disabled"
                  class="w-10 h-5 rounded-full transition-colors relative flex-shrink-0"
                  :class="!settingsDraft.wake_word_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                  <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                    :class="!settingsDraft.wake_word_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
                </button>
              </div>
            </div>
            <div>
              <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Recognition language</label>
              <select v-model="settingsDraft.voice_language"
                class="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors">
                <option value="auto">Auto — Russian &amp; English</option>
                <option value="ru">Russian only</option>
                <option value="en">English only</option>
              </select>
              <p class="text-[10px] text-gray-600 mt-1">«Auto» detects the language per phrase. Choose a fixed language if recognition is unstable.</p>
            </div>
            <div>
              <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Weather city</label>
              <div class="flex gap-2">
                <input v-model="settingsDraft.voice_city" placeholder="Москва"
                  class="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors" />
                <button @click="applyVoiceCity" class="btn-ghost text-xs px-3">SET</button>
                <button @click="detectVoiceLocation" class="btn-ghost text-xs px-3">DETECT</button>
              </div>
            </div>
            <div class="grid grid-cols-2 gap-2 text-xs">
              <button type="button" :aria-pressed="!settingsDraft.voice_confirm_dangerous_disabled"
                @click="settingsDraft.voice_confirm_dangerous_disabled = !settingsDraft.voice_confirm_dangerous_disabled"
                class="rounded-lg px-3 py-2 text-left transition-colors"
                :class="!settingsDraft.voice_confirm_dangerous_disabled ? 'bg-cyan-500/15 text-cyan-300' : 'bg-gray-900 text-gray-500'">
                Confirm: {{ settingsDraft.voice_confirm_dangerous_disabled ? 'off' : 'on' }}
              </button>
              <button type="button" :aria-pressed="!settingsDraft.voice_agent_disabled"
                @click="settingsDraft.voice_agent_disabled = !settingsDraft.voice_agent_disabled"
                class="rounded-lg px-3 py-2 text-left transition-colors"
                :class="!settingsDraft.voice_agent_disabled ? 'bg-cyan-500/15 text-cyan-300' : 'bg-gray-900 text-gray-500'">
                Agent: {{ settingsDraft.voice_agent_disabled ? 'off' : 'on' }}
              </button>
            </div>
            <div class="rounded-lg bg-gray-900/70 border border-gray-800 p-3 space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-xs text-gray-500 uppercase tracking-wider">Microphone check</span>
                <button @click="checkMicrophone" :disabled="micChecking" class="btn-ghost text-xs px-3">
                  {{ micChecking ? 'CHECKING...' : 'CHECK' }}
                </button>
              </div>
              <template v-if="micDiagnostics">
                <p class="text-xs" :class="micDiagnostics.signal_detected ? 'text-cyan-300' : 'text-amber-400'">
                  {{ micDiagnostics.verdict }}
                </p>
                <p class="text-[10px] text-gray-600 font-mono">
                  devices: {{ micDiagnostics.device_count }} ·
                  active: {{ micDiagnostics.active_endpoints }} ·
                  disabled: {{ micDiagnostics.disabled_endpoints }} ·
                  privacy: {{ micDiagnostics.privacy_allowed ? 'allowed' : 'blocked' }} ·
                  peak: {{ (micDiagnostics.peak * 100).toFixed(1) }}%
                </p>
                <p v-if="micDiagnostics.selected_device" class="text-[10px] text-gray-600">
                  Input: {{ micDiagnostics.selected_device }}
                </p>
                <ul v-if="micDiagnostics.hints.length" class="text-[11px] text-amber-400/80 space-y-0.5 list-disc pl-4">
                  <li v-for="hint in micDiagnostics.hints" :key="hint">{{ hint }}</li>
                </ul>
                <p v-if="micDiagnostics.error" class="text-[11px] text-red-400">{{ micDiagnostics.error }}</p>
              </template>
              <p v-else class="text-[11px] text-gray-600">
                Check whether Windows sees a microphone, allows access, and receives sound.
              </p>
            </div>
            <p v-if="voiceHeard" class="text-xs text-gray-600">Heard: {{ voiceHeard }}</p>
            <p v-if="voiceError" class="text-xs text-red-400">{{ voiceError }}</p>
          </div>
          <div class="flex items-center justify-between">
            <div>
              <span class="text-sm text-gray-400">Web search</span>
              <p class="text-xs text-gray-600 mt-0.5">Sends queries to DuckDuckGo</p>
            </div>
            <button type="button" role="switch" :aria-checked="settingsDraft.web_search"
              aria-label="Web search" @click="settingsDraft.web_search = !settingsDraft.web_search"
              class="w-10 h-5 rounded-full transition-colors relative"
              :class="settingsDraft.web_search ? 'bg-cyan-500' : 'bg-gray-700'">
              <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                :class="settingsDraft.web_search ? 'translate-x-5' : 'translate-x-0.5'"></span>
            </button>
          </div>
          <div class="flex items-center justify-between">
            <div>
              <span class="text-sm text-gray-400">Image generation</span>
              <p class="text-xs text-gray-600 mt-0.5">Sends prompts to Pollinations.ai (opt-in)</p>
            </div>
            <button type="button" role="switch" :aria-checked="settingsDraft.image_generation_enabled"
              aria-label="Image generation" @click="settingsDraft.image_generation_enabled = !settingsDraft.image_generation_enabled"
              class="w-10 h-5 rounded-full transition-colors relative"
              :class="settingsDraft.image_generation_enabled ? 'bg-cyan-500' : 'bg-gray-700'">
              <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                :class="settingsDraft.image_generation_enabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
            </button>
          </div>
          <div class="rounded-lg bg-gray-900/70 border border-gray-800 p-3 space-y-2">
            <div class="flex items-center justify-between">
              <span class="text-xs text-gray-500 uppercase tracking-wider">Application catalog</span>
              <button @click="scanApps" :disabled="appScanning" class="btn-ghost text-xs px-3">
                {{ appScanning ? 'SCANNING...' : 'RESCAN' }}
              </button>
            </div>
            <p class="text-[11px] text-gray-500">
              {{ catalogStatus.count }} applications indexed · {{ catalogUpdatedLabel() }}
            </p>
            <p class="text-[10px] text-gray-600">
              JARVIS can open any indexed application by voice or with /apps. The sidebar keeps a short curated list.
            </p>
          </div>
          <div class="rounded-lg bg-gray-900/70 border border-gray-800 p-3 space-y-2">
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm text-gray-400">Auto-start with Windows</span>
                <p class="text-xs text-gray-600 mt-0.5">Starts hidden in the tray, ready for wake word and Ctrl+Shift+J</p>
              </div>
              <button type="button" role="switch" :aria-checked="autoStartDraft"
                aria-label="Auto-start with Windows" @click="autoStartDraft = !autoStartDraft"
                class="w-10 h-5 rounded-full transition-colors relative flex-shrink-0"
                :class="autoStartDraft ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="autoStartDraft ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
            <p v-if="autoStartStatus" class="text-[10px] text-gray-600 font-mono break-all">
              mode: {{ autoStartStatus.mode }} ·
              exe: {{ autoStartStatus.executable_exists ? 'present' : 'missing' }}
            </p>
            <p v-if="autoStartStatus?.startup_approved_disabled" class="text-[11px] text-amber-400">
              Windows has disabled this entry in Task Manager. Saving the setting repairs it.
            </p>
            <div class="flex items-center gap-3">
              <button type="button" @click="testAutoStart" :disabled="autoStartTesting" class="btn-ghost text-xs px-3">
                {{ autoStartTesting ? 'TESTING...' : 'TEST START' }}
              </button>
              <span v-if="autoStartTestMessage" class="text-[11px]" :class="autoStartTestOK ? 'text-cyan-300' : 'text-amber-400'">
                {{ autoStartTestMessage }}
              </span>
            </div>
          </div>
          <div class="rounded-lg bg-gray-900/70 border border-gray-800 p-3 space-y-3">
            <div>
              <span class="text-xs text-gray-500 uppercase tracking-wider">Proactivity</span>
              <p class="text-[10px] text-gray-600 mt-0.5">How much JARVIS does without asking. Dangerous actions always need confirmation.</p>
            </div>
            <div class="grid grid-cols-3 gap-2 text-xs">
              <button type="button" :aria-pressed="settingsDraft.autonomy_level === 'off'"
                @click="settingsDraft.autonomy_level = 'off'"
                class="rounded-lg px-2 py-2 text-left transition-colors"
                :class="settingsDraft.autonomy_level === 'off' ? 'bg-cyan-500/15 text-cyan-300' : 'bg-gray-900 text-gray-500'">
                OFF
              </button>
              <button type="button" :aria-pressed="settingsDraft.autonomy_level === 'suggestions'"
                @click="settingsDraft.autonomy_level = 'suggestions'"
                class="rounded-lg px-2 py-2 text-left transition-colors"
                :class="settingsDraft.autonomy_level === 'suggestions' ? 'bg-cyan-500/15 text-cyan-300' : 'bg-gray-900 text-gray-500'">
                SUGGEST
              </button>
              <button type="button" :aria-pressed="settingsDraft.autonomy_level === 'trusted'"
                @click="settingsDraft.autonomy_level = 'trusted'"
                class="rounded-lg px-2 py-2 text-left transition-colors"
                :class="settingsDraft.autonomy_level === 'trusted' ? 'bg-cyan-500/15 text-cyan-300' : 'bg-gray-900 text-gray-500'">
                TRUSTED
              </button>
            </div>
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm text-gray-400">Startup briefing</span>
                <p class="text-xs text-gray-600 mt-0.5">Time, weather, reminders and game status on launch</p>
              </div>
              <button type="button" role="switch" :aria-checked="!settingsDraft.proactive_briefing_disabled"
                aria-label="Startup briefing" @click="settingsDraft.proactive_briefing_disabled = !settingsDraft.proactive_briefing_disabled"
                class="w-10 h-5 rounded-full transition-colors relative flex-shrink-0"
                :class="!settingsDraft.proactive_briefing_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="!settingsDraft.proactive_briefing_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm text-gray-400">Game detection</span>
                <p class="text-xs text-gray-600 mt-0.5">Tracks foreground games and playtime locally</p>
              </div>
              <button type="button" role="switch" :aria-checked="!settingsDraft.game_watch_disabled"
                aria-label="Game detection" @click="settingsDraft.game_watch_disabled = !settingsDraft.game_watch_disabled"
                class="w-10 h-5 rounded-full transition-colors relative flex-shrink-0"
                :class="!settingsDraft.game_watch_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="!settingsDraft.game_watch_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm text-gray-400">Break reminders</span>
                <p class="text-xs text-gray-600 mt-0.5">Suggests a break after an hour of play</p>
              </div>
              <button type="button" role="switch" :aria-checked="!settingsDraft.game_break_reminders_disabled"
                aria-label="Break reminders" @click="settingsDraft.game_break_reminders_disabled = !settingsDraft.game_break_reminders_disabled"
                class="w-10 h-5 rounded-full transition-colors relative flex-shrink-0"
                :class="!settingsDraft.game_break_reminders_disabled ? 'bg-cyan-500' : 'bg-gray-700'">
                <span class="absolute w-4 h-4 bg-white rounded-full top-0.5 transition-transform"
                  :class="!settingsDraft.game_break_reminders_disabled ? 'translate-x-5' : 'translate-x-0.5'"></span>
              </button>
            </div>
            <p v-if="gameStatus?.playing" class="text-[11px] text-cyan-300">
              Playing {{ gameStatus.name }} · session {{ gameDuration(gameStatus.session_seconds) }} · today {{ gameDuration(gameStatus.today_seconds) }}
            </p>
            <p v-else class="text-[11px] text-gray-600">No game detected right now.</p>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-sm text-gray-400">Global Hotkey</span>
            <span class="text-xs text-gray-600 font-mono">Ctrl+Shift+J</span>
          </div>
          <p v-if="!status.hotkey_active" class="text-xs text-amber-400">
            Hotkey is unavailable{{ status.hotkey_error ? ': ' + status.hotkey_error : '' }}. Use the tray icon to open JARVIS.
          </p>
          <p v-if="status.history_error" class="text-xs text-amber-400">{{ status.history_error }}</p>
          <p v-if="status.settings_error" class="text-xs text-amber-400">{{ status.settings_error }}</p>
          <p v-if="settingsError" class="text-xs text-red-400">{{ settingsError }}</p>
          <div class="flex gap-3 pt-2">
            <button @click="saveSettings" class="btn-neon flex-1">SAVE</button>
            <button @click="closeSettings" class="btn-ghost flex-1">CANCEL</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Image Modal -->
    <div class="modal-overlay" v-if="showImageModal" @click.self="closeImageModal">
      <div class="modal-panel glass-strong neon-glow" role="dialog" aria-modal="true" aria-label="Generate image">
        <h2>GENERATE IMAGE</h2>
        <div class="space-y-4">
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Description</label>
            <textarea v-model="imagePrompt" placeholder="A cyberpunk city at night, neon lights, rain..."
              rows="3" class="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors resize-none"
              @keydown="(e: KeyboardEvent) => { if (e.key === 'Enter' && e.ctrlKey) generateImage() }"></textarea>
          </div>
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wider block mb-1">Size</label>
            <select v-model="imageSize" class="w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors">
              <option value="512x512">512 &times; 512 (Square)</option>
              <option value="1024x1024">1024 &times; 1024 (Medium)</option>
              <option value="1024x768">1024 &times; 768 (Landscape)</option>
              <option value="768x1024">768 &times; 1024 (Portrait)</option>
              <option value="1536x1024">1536 &times; 1024 (Wide)</option>
            </select>
          </div>
          <div class="flex gap-3 pt-2">
            <button @click="generateImage" :disabled="imageLoading || !imagePrompt.trim()" class="btn-neon flex-1">
              {{ imageLoading ? 'Generating...' : 'GENERATE' }}
            </button>
            <button @click="closeImageModal" class="btn-ghost flex-1">CANCEL</button>
          </div>
        </div>
      </div>
    </div>

    <!-- First-launch weather location consent -->
    <div class="modal-overlay" v-if="showLocationPrompt && !showSetup" @click.self="dismissLocationPrompt" style="z-index:150">
      <div class="modal-panel glass-strong neon-glow" role="dialog" aria-modal="true" aria-label="Weather location" style="max-width:520px">
        <div class="text-xs tracking-[0.3em] text-cyan-500 uppercase">Weather module</div>
        <h2 class="mt-2">WHERE ARE WE, SIR?</h2>
        <p class="text-sm text-gray-400 mt-3 leading-relaxed">
          Мой метеорологический модуль не знает, где вы находитесь. Разрешите определить
          город по сети — я не стану спрашивать координаты у спутников. Или укажите город
          вручную.
        </p>
        <button @click="useIpLocation" :disabled="locationBusy" class="btn-neon w-full mt-4">
          {{ locationBusy ? 'DETECTING...' : 'DETECT AUTOMATICALLY' }}
        </button>
        <div class="flex gap-2 mt-2">
          <input v-model="locationCity" placeholder="Москва" @keyup.enter="saveLocationCity"
            class="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-cyan-500 transition-colors" />
          <button @click="saveLocationCity" :disabled="locationBusy" class="btn-ghost px-4">SAVE CITY</button>
        </div>
        <p v-if="locationError" class="text-xs text-red-400 mt-2">{{ locationError }}</p>
        <button @click="dismissLocationPrompt" class="text-xs text-gray-600 hover:text-gray-400 mt-3">Позже</button>
      </div>
    </div>

    <!-- Screen Analysis Consent -->
    <div class="modal-overlay" v-if="showScreenConfirm" @click.self="closeScreenConfirm">
      <div class="modal-panel glass-strong neon-glow" role="dialog" aria-modal="true" aria-label="Screen analysis">
        <h2>SCREEN ANALYSIS</h2>
        <p class="text-sm text-gray-400">JARVIS will capture the current screen and send the image to your local Ollama model. Continue?</p>
        <p v-if="screenQuestion" class="text-xs text-gray-500 mt-2">Question: {{ screenQuestion }}</p>
        <div class="flex gap-3 pt-4">
          <button @click="confirmScreenAnalysis" class="btn-neon flex-1">ALLOW</button>
          <button @click="closeScreenConfirm" class="btn-ghost flex-1">CANCEL</button>
        </div>
      </div>
    </div>

    <!-- Setup Wizard Overlay -->
    <div class="modal-overlay" v-if="showSetup" style="z-index:200">
      <div class="modal-panel glass-strong neon-glow" role="dialog" aria-modal="true" aria-label="Automatic setup" style="max-width:620px">
        <template v-if="setupStep === 0 || setupStep === 1">
          <div class="text-center py-6">
            <h2 style="font-size:24px;font-weight:700;background:linear-gradient(135deg,#00d4ff,#0066ff);-webkit-background-clip:text;-webkit-text-fill-color:transparent">JARVIS</h2>
            <p class="text-gray-400 mt-2 text-sm">Scanning hardware...</p>
            <div class="typing-indicator flex gap-1.5 justify-center mt-6">
              <span></span><span></span><span></span>
            </div>
          </div>
        </template>

        <template v-else-if="setupStep === 2">
          <div class="text-center">
            <h2 class="text-xl font-bold">AUTOMATIC SETUP</h2>
            <p class="text-gray-400 text-sm mt-1">JARVIS detected your hardware and selected a model that fits.</p>
          </div>

          <div class="glass rounded-xl p-4 mt-4" style="border:1px solid rgba(0,212,255,0.15)">
            <div class="text-xs text-gray-500 uppercase tracking-wider mb-2">Hardware</div>
            <div class="grid grid-cols-2 gap-3 text-xs">
              <div>
                <div class="text-gray-500">CPU</div>
                <div class="text-white mt-0.5">{{ setupInfo?.cpu_name || 'Detecting...' }}</div>
                <div class="text-gray-600">{{ setupInfo?.cpu_cores || '?' }} cores · {{ setupInfo?.cpu_threads || '?' }} threads</div>
              </div>
              <div>
                <div class="text-gray-500">GPU</div>
                <div class="text-white mt-0.5">{{ setupInfo?.gpu_name || 'Not detected' }}</div>
                <div v-if="setupInfo?.gpu_vram_gb" class="text-gray-600">
                  {{ setupInfo.gpu_vram_gb }} GB VRAM<span v-if="setupInfo?.gpu_integrated"> · integrated</span>
                </div>
                <div v-else class="text-gray-600">CPU inference</div>
              </div>
              <div>
                <div class="text-gray-500">RAM</div>
                <div class="text-white mt-0.5">{{ setupInfo?.ram_gb || '?' }} GB</div>
              </div>
              <div>
                <div class="text-gray-500">Free disk</div>
                <div class="text-white mt-0.5">{{ setupInfo?.disk_free_gb || '?' }} GB</div>
              </div>
            </div>
          </div>

          <div class="glass rounded-xl p-4 mt-3" style="border:1px solid rgba(0,212,255,0.15)">
            <div class="flex items-start justify-between gap-3">
              <div>
                <div class="text-xs text-gray-500 uppercase tracking-wider mb-1">Recommended model</div>
                <div class="text-lg font-bold text-white">{{ setupModel?.model || '...' }}</div>
                <p class="text-sm text-gray-400 mt-1">{{ setupModel?.description }}</p>
              </div>
              <span class="text-xs px-2 py-1 rounded-full flex-shrink-0"
                :style="setupModel?.device === 'GPU' ? 'background:rgba(0,255,136,0.12);color:#00ff88' : 'background:rgba(0,212,255,0.12);color:#00d4ff'">
                {{ setupModel?.device || 'CPU' }}
              </span>
            </div>
            <div class="flex flex-wrap gap-x-4 gap-y-1 mt-3 text-xs text-gray-500">
              <span>Download: {{ setupModel?.size || '?' }}</span>
              <span>RAM: {{ setupModel?.ram_required || '?' }}+ GB</span>
              <span v-if="setupModel?.vram_required_gb">VRAM: {{ setupModel?.vram_required_gb }}+ GB</span>
            </div>
          </div>

          <div class="mt-3 text-xs text-gray-500 leading-relaxed">
            <p v-if="setupPlan?.needs_install">
              Ollama is not installed. JARVIS will install it automatically (~{{ setupPlan?.installer_download_gb }} GB). Windows may ask for permission.
            </p>
            <p v-else-if="setupPlan?.needs_start">Ollama is installed and will be started automatically.</p>
            <p v-if="setupPlan?.needs_model">The selected model will be verified and downloaded if needed.</p>
            <p v-else-if="!setupPlan?.current_model_installed && setupPlan?.recommended_installed">
              The configured model is missing. JARVIS will switch to the recommended installed model.
            </p>
            <p v-if="setupPlan?.ready" class="text-green-400">Everything is ready.</p>
          </div>

          <p v-if="setupError" class="text-xs text-red-400 mt-3">{{ setupError }}</p>

          <div class="mt-5 flex flex-col gap-3">
            <button @click="runAutomaticSetup" :disabled="setupRunning" class="btn-neon w-full">
              {{ setupRunning ? 'SETTING UP...' : 'SET UP AUTOMATICALLY' }}
            </button>
            <button @click="finishSetup" class="btn-ghost w-full">SKIP FOR NOW</button>
          </div>
        </template>

        <template v-else-if="setupStep === 3">
          <div class="text-center">
            <h2 class="text-xl font-bold">SETTING UP JARVIS</h2>
            <p class="text-sm text-gray-300 mt-2">{{ setupProgress.message }}</p>
          </div>
          <div class="mt-6" style="height:8px;background:rgba(255,255,255,0.08);border-radius:999px;overflow:hidden">
            <div :style="{ width: progressWidth(), height:'100%', background:'linear-gradient(90deg,#00bfff,#0066ff)', transition:'width 0.3s ease', boxShadow:'0 0 12px rgba(0,191,255,0.5)' }"></div>
          </div>
          <div class="flex justify-between mt-2 text-xs text-gray-500">
            <span>{{ setupProgress.percent }}%</span>
            <span class="truncate ml-3">{{ setupProgress.detail || setupProgress.stage }}</span>
          </div>
          <p class="text-xs text-gray-600 mt-5 text-center">
            Keep this window open. Installing Ollama and downloading large models can take several minutes.
          </p>
        </template>

        <template v-else-if="setupStep === 4">
          <div class="text-center py-4">
            <h2 class="text-xl font-bold">Ready, Sir.</h2>
            <p class="text-gray-400 text-sm mt-2">JARVIS is fully operational.</p>
            <div class="mt-4 glass rounded-xl p-4" style="border:1px solid rgba(0,212,255,0.15)">
              <div class="text-xs text-gray-500 uppercase tracking-wider mb-1">Active model</div>
              <div class="text-lg font-bold text-white">{{ setupPlan?.selected_model || setupModel?.model }}</div>
              <div class="text-xs text-gray-500 mt-1">{{ setupModel?.device || 'CPU' }} · {{ setupModel?.size || '' }}</div>
            </div>
            <div class="mt-6">
              <button @click="finishSetup" class="btn-neon w-full">BEGIN</button>
            </div>
          </div>
        </template>

        <template v-else-if="setupStep === 5">
          <div class="text-center py-2">
            <h2 class="text-xl font-bold">SETUP NEEDS ATTENTION</h2>
            <p class="text-gray-400 text-sm mt-2">Automatic setup could not finish.</p>
          </div>
          <div class="mt-4 glass rounded-xl p-4 text-left" style="border:1px solid rgba(255,68,68,0.25)">
            <p class="text-xs text-red-300 break-words">{{ setupError || 'Unknown setup error.' }}</p>
          </div>
          <div class="mt-5 flex flex-col gap-3">
            <button @click="retrySetup" :disabled="setupRunning" class="btn-neon w-full">RETRY AUTOMATIC SETUP</button>
            <button v-if="downloadURL" @click="openExternal(downloadURL)" class="btn-ghost w-full">OPEN OLLAMA DOWNLOAD PAGE</button>
            <button @click="finishSetup" class="btn-ghost w-full">SKIP FOR NOW</button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

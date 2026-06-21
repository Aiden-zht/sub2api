<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { adminThemesAPI, type ThemePackage } from '@/api/themes'
import { useAppStore } from '@/stores/app'
import { useI18n } from 'vue-i18n'
import { refreshActiveThemePackage } from '@/utils/themePackage'

const { t } = useI18n()
const appStore = useAppStore()

const themes = ref<ThemePackage[]>([])
const loading = ref(false)
const uploading = ref(false)
const actionId = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

async function loadThemes() {
  loading.value = true
  try {
    const data = await adminThemesAPI.list()
    themes.value = data.themes || []
  } catch (error) {
    appStore.showError((error as { message?: string }).message || t('admin.themes.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openUpload() {
  fileInput.value?.click()
}

async function onFileSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  uploading.value = true
  try {
    await adminThemesAPI.upload(file)
    await refreshActiveThemePackage()
    appStore.showSuccess(t('admin.themes.uploadSuccess'))
    await loadThemes()
  } catch (error) {
    appStore.showError((error as { message?: string }).message || t('admin.themes.uploadFailed'))
  } finally {
    uploading.value = false
  }
}

async function runThemeAction(id: string, action: () => Promise<void>, successKey: string) {
  actionId.value = id
  try {
    await action()
    await refreshActiveThemePackage()
    appStore.showSuccess(t(successKey))
    await loadThemes()
  } catch (error) {
    appStore.showError((error as { message?: string }).message || t('admin.themes.actionFailed'))
  } finally {
    actionId.value = ''
  }
}

function enableTheme(theme: ThemePackage) {
  return runThemeAction(theme.id, () => adminThemesAPI.enable(theme.id), 'admin.themes.enableSuccess')
}

function disableTheme(theme: ThemePackage) {
  return runThemeAction(theme.id, () => adminThemesAPI.disable(theme.id), 'admin.themes.disableSuccess')
}

function deleteTheme(theme: ThemePackage) {
  if (!window.confirm(t('admin.themes.deleteConfirm', { name: theme.name }))) return
  return runThemeAction(theme.id, () => adminThemesAPI.delete(theme.id), 'admin.themes.deleteSuccess')
}

onMounted(loadThemes)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.themes.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.themes.description') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="loadThemes">
          {{ t('common.refresh') }}
        </button>
        <button class="btn btn-primary" :disabled="uploading" @click="openUpload">
          {{ uploading ? t('admin.themes.uploading') : t('admin.themes.upload') }}
        </button>
        <input ref="fileInput" type="file" accept=".zip" class="hidden" @change="onFileSelected" />
      </div>
    </div>

    <div class="card overflow-hidden">
      <div v-if="loading" class="p-6 text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="themes.length === 0" class="p-10 text-center">
        <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.themes.empty') }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.themes.emptyHint') }}</p>
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 text-left text-xs font-semibold uppercase text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
            <tr>
              <th class="px-5 py-3">{{ t('admin.themes.theme') }}</th>
              <th class="px-5 py-3">{{ t('admin.themes.version') }}</th>
              <th class="px-5 py-3">{{ t('admin.themes.capabilities') }}</th>
              <th class="px-5 py-3">{{ t('admin.themes.status') }}</th>
              <th class="px-5 py-3 text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="theme in themes" :key="theme.id" class="bg-white dark:bg-dark-800/40">
              <td class="px-5 py-4">
                <div class="font-medium text-gray-900 dark:text-white">{{ theme.name }}</div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ theme.id }}</div>
                <div v-if="theme.manifest.description" class="mt-1 max-w-xl text-xs text-gray-500 dark:text-gray-400">
                  {{ theme.manifest.description }}
                </div>
              </td>
              <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ theme.version }}</td>
              <td class="px-5 py-4">
                <div class="flex flex-wrap gap-1">
                  <span
                    v-for="cap in theme.manifest.capabilities || ['css', 'assets']"
                    :key="cap"
                    class="rounded border border-gray-200 px-2 py-0.5 text-xs text-gray-600 dark:border-dark-600 dark:text-gray-300"
                  >
                    {{ cap }}
                  </span>
                </div>
              </td>
              <td class="px-5 py-4">
                <span
                  class="rounded px-2 py-1 text-xs font-medium"
                  :class="theme.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
                >
                  {{ theme.enabled ? t('admin.themes.enabled') : t('admin.themes.disabled') }}
                </span>
              </td>
              <td class="px-5 py-4">
                <div class="flex justify-end gap-2">
                  <button v-if="!theme.enabled" class="btn btn-secondary btn-sm" :disabled="actionId === theme.id" @click="enableTheme(theme)">
                    {{ t('admin.themes.enable') }}
                  </button>
                  <button v-else class="btn btn-secondary btn-sm" :disabled="actionId === theme.id" @click="disableTheme(theme)">
                    {{ t('admin.themes.disable') }}
                  </button>
                  <button class="btn btn-danger btn-sm" :disabled="actionId === theme.id" @click="deleteTheme(theme)">
                    {{ t('common.delete') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

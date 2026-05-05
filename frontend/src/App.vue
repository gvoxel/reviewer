<template>
  <div class="min-h-screen bg-surface-alt">
    <header class="bg-surface border-b border-edge sticky top-0 z-50">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex items-center h-14">
          <router-link to="/" class="flex items-center gap-2 text-fg hover:text-accent transition-colors">
            <span class="text-lg font-bold tracking-tight relative top-[-1px]">reviewer</span>
          </router-link>

          <nav class="ml-6 flex items-center gap-3 text-sm font-medium">
            <span class="text-accent">Reviews</span>
            <a href="/vt/" class="text-fg-subtle hover:text-fg-secondary transition-colors">Settings</a>
          </nav>

          <span class="mx-2 sm:mx-4 h-5 w-px bg-edge"></span>

          <!-- Breadcrumbs -->
          <nav class="flex items-center gap-1.5 text-sm text-fg-subtle min-w-0">
            <router-link to="/" class="hover:text-fg-secondary transition-colors flex-shrink-0">Projects</router-link>
            <template v-if="breadcrumbs.project">
              <span class="flex-shrink-0">/</span>
              <router-link
                :to="{ name: 'reviews', params: { id: breadcrumbs.project.id } }"
                class="hover:text-fg-secondary transition-colors truncate max-w-[120px] sm:max-w-[200px]"
                :title="breadcrumbs.project.title"
              >{{ breadcrumbs.project.title }}</router-link>
            </template>
            <template v-if="breadcrumbs.review">
              <span class="flex-shrink-0">/</span>
              <span class="text-fg-secondary truncate max-w-[150px] sm:max-w-[300px]" :title="`#${breadcrumbs.review.id} ${breadcrumbs.review.title}`">
                #{{ breadcrumbs.review.id }} {{ breadcrumbs.review.title }}
              </span>
            </template>
          </nav>

          <div class="ml-auto flex items-center gap-3">
            <button @click="toggle" class="text-fg-subtle hover:text-fg-secondary transition-colors" :title="isDark ? 'Light mode' : 'Dark mode'">
              <!-- Moon icon (shown in light mode) -->
              <svg v-if="!isDark" xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                <path d="M17.293 13.293A8 8 0 016.707 2.707a8.001 8.001 0 1010.586 10.586z" />
              </svg>
              <!-- Sun icon (shown in dark mode) -->
              <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M10 2a1 1 0 011 1v1a1 1 0 11-2 0V3a1 1 0 011-1zm4 8a4 4 0 11-8 0 4 4 0 018 0zm-.464 4.95l.707.707a1 1 0 001.414-1.414l-.707-.707a1 1 0 00-1.414 1.414zm2.12-10.607a1 1 0 010 1.414l-.706.707a1 1 0 11-1.414-1.414l.707-.707a1 1 0 011.414 0zM17 11a1 1 0 100-2h-1a1 1 0 100 2h1zm-7 4a1 1 0 011 1v1a1 1 0 11-2 0v-1a1 1 0 011-1zM5.05 6.464A1 1 0 106.465 5.05l-.708-.707a1 1 0 00-1.414 1.414l.707.707zm1.414 8.486l-.707.707a1 1 0 01-1.414-1.414l.707-.707a1 1 0 011.414 1.414zM4 11a1 1 0 100-2H3a1 1 0 000 2h1z" clip-rule="evenodd" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </header>

    <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useBreadcrumbs } from './composables/useBreadcrumbs'
import { useTheme } from './composables/useTheme'

const { breadcrumbs } = useBreadcrumbs()
const { isDark, toggle } = useTheme()
</script>

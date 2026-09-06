<template>
  <component :is="viewComponent" />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const AccountsView = defineAsyncComponent(() => import('./AccountsView.vue'))
const MoshuUpstreamView = defineAsyncComponent(() => import('./MoshuUpstreamView.vue'))

const viewComponent = computed(() =>
  appStore.cachedPublicSettings?.moshu_only_mode === true ? MoshuUpstreamView : AccountsView
)
</script>

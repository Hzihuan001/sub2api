<template>
  <component :is="viewComponent" />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const GroupsView = defineAsyncComponent(() => import('./GroupsView.vue'))
const MoshuPricingView = defineAsyncComponent(() => import('./MoshuPricingView.vue'))

const viewComponent = computed(() =>
  appStore.cachedPublicSettings?.moshu_only_mode === true ? MoshuPricingView : GroupsView
)
</script>

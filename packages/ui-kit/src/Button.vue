<script setup lang="ts">
import { cva } from 'class-variance-authority'
import { cn } from './cn'

// 注意：不要写成 defineProps<VariantProps<typeof button>>()。
// Vue 3.5 的 SFC 编译器无法解析 cva 的复杂条件类型，必须使用显式联合类型。
const button = cva('inline-flex items-center justify-center rounded-card px-4 py-2 text-sm font-medium', {
  variants: {
    intent: {
      primary: 'bg-brand-500 text-white hover:bg-brand-600',
      ghost: 'bg-transparent text-brand-600',
    },
  },
  defaultVariants: { intent: 'primary' },
})

const props = withDefaults(defineProps<{ intent?: 'primary' | 'ghost' }>(), {
  intent: 'primary',
})
</script>

<template>
  <button :class="cn(button({ intent: props.intent }))">
    <slot />
  </button>
</template>

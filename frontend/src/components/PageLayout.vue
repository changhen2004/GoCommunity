<template>
  <div class="switch-container page-shell">
    <aside v-if="$slots.aside" class="ioui-aside">
      <div class="aside-card blur-bg">
        <ul class="aside-ul no-scrollbar">
          <slot name="aside" />
        </ul>
      </div>
    </aside>

    <div class="content-wrap">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
defineSlots<{
  default: () => unknown;
  aside?: () => unknown;
}>();
</script>

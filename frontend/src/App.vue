<template>
  <div class="app-shell">
    <SiteHeader />

    <main class="app-main">
      <router-view />
    </main>

    <footer class="main-footer">
      <div class="switch-container">
        <div class="footer-copyright">
          <p>资源社区 · 分享、解锁、沉淀内容价值</p>
          <p class="text-xs">本站资源由社区成员发布，仅用于学习与交流，请勿用于商业用途。</p>
        </div>
      </div>
    </footer>

    <div class="tools-right">
      <button
        v-if="showBackToTop"
        type="button"
        class="btn-tools"
        title="返回顶部"
        aria-label="返回顶部"
        @click="scrollToTop"
      >
        <Top />
      </button>
      <button
        type="button"
        class="btn-tools"
        :title="isDark ? '日间模式' : '夜间模式'"
        aria-label="切换夜间模式"
        @click="toggleTheme"
      >
        <component :is="isDark ? Sunny : Moon" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { Moon, Sunny, Top } from '@element-plus/icons-vue';
import SiteHeader from './components/SiteHeader.vue';

const THEME_KEY = 'theme-mode';

const isDark = ref(false);
const showBackToTop = ref(false);

const applyTheme = (dark: boolean) => {
  isDark.value = dark;
  document.documentElement.classList.toggle('dark-mode', dark);
};

const toggleTheme = () => {
  const next = !isDark.value;
  applyTheme(next);
  localStorage.setItem(THEME_KEY, next ? 'dark' : 'light');
};

const scrollToTop = () => {
  window.scrollTo({ top: 0, behavior: 'smooth' });
};

const onScroll = () => {
  showBackToTop.value = window.scrollY > 300;
};

onMounted(() => {
  const stored = localStorage.getItem(THEME_KEY);
  const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
  applyTheme(stored ? stored === 'dark' : prefersDark);
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });
});

onUnmounted(() => {
  window.removeEventListener('scroll', onScroll);
});
</script>

<style scoped>
.app-shell {
  min-height: 100vh;
}

.app-main {
  min-height: calc(100vh - var(--main-nav-hight) - 190px);
}

.footer-copyright p {
  margin: 0 0 4px;
}
</style>

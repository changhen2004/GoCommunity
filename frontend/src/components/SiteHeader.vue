<template>
  <div>
    <header class="main-header">
      <div class="header-nav blur-bg" :class="{ 'is-scrolled': scrolled }">
        <nav class="header-nav__inner">
          <button type="button" class="menu-btn" aria-label="打开菜单" @click="mobileOpen = true">
            <span class="menu-bar"></span>
            <span class="menu-bar"></span>
            <span class="menu-bar"></span>
          </button>

          <div class="navbar-logo">
            <button type="button" class="navbar-logo__btn" @click="goTo('Home')">
              <span class="navbar-logo__mark">RC</span>
              <span class="navbar-logo__text">
                <strong>资源社区</strong>
                <small>分享、解锁、沉淀内容价值</small>
              </span>
            </button>
          </div>

          <ul class="navbar-header">
            <li :class="{ 'is-active': route.name === 'Home' }">
              <button type="button" @click="goTo('Home')">
                <House class="nav-ico" />
                <span>首页</span>
              </button>
            </li>
            <li :class="{ 'is-active': route.name === 'Resources' }">
              <button type="button" @click="goTo('Resources')">
                <Grid class="nav-ico" />
                <span>资源广场</span>
                <ArrowDown class="caret" />
              </button>
              <ul class="sub-menu">
                <li v-for="feed in feedEntries" :key="feed.value">
                  <button type="button" @click="goToFeed(feed.value)">
                    <component :is="feed.icon" class="nav-ico" />
                    <span>{{ feed.label }}</span>
                  </button>
                </li>
              </ul>
            </li>
            <li :class="{ 'is-active': route.name === 'Center' }">
              <button type="button" @click="goTo('Center')">
                <User class="nav-ico" />
                <span>个人中心</span>
              </button>
            </li>
          </ul>

          <ul class="header-tools">
            <li class="header-icon-btn">
              <button type="button" aria-label="搜索" @click="openSearch">
                <Search />
              </button>
            </li>

            <li class="header-icon-btn nav-login">
              <button type="button" aria-label="用户菜单">
                <User />
              </button>
              <ul class="sub-menu sub-menu--right">
                <li class="nav-user-box">
                  <div class="user-info">
                    <div class="avatar-img">
                      <span>{{ userInitial }}</span>
                    </div>
                    <div class="user-right">
                      <b>{{ authStore.isAuthenticated ? displayName : '未登录' }}</b>
                      <div class="text-xs line1">
                        {{
                          authStore.isAuthenticated
                            ? `${authStore.pointsBalance} 积分可用`
                            : '登录后即可体验更多功能'
                        }}
                      </div>
                    </div>
                  </div>

                  <div v-if="authStore.isAuthenticated" class="user-btn">
                    <button type="button" class="btn l-vc-blue" @click="goTo('Center')">
                      <User />
                      <span>个人中心</span>
                    </button>
                    <button type="button" class="btn l-vc-green" @click="goTo('CreateResource')">
                      <Plus />
                      <span>发布资源</span>
                    </button>
                    <button type="button" class="btn l-vc-theme" @click="handleLogout">
                      <SwitchButton />
                      <span>退出</span>
                    </button>
                  </div>

                  <div v-else class="user-btn">
                    <button type="button" class="btn l-vc-blue" @click="goTo('Login')">
                      <User />
                      <span>登录</span>
                    </button>
                    <button type="button" class="btn l-vc-green" @click="goTo('Register')">
                      <EditPen />
                      <span>注册</span>
                    </button>
                  </div>
                </li>
              </ul>
            </li>
          </ul>
        </nav>
      </div>
    </header>

    <div class="mobile-header" :class="{ 'is-mobile': mobileOpen }">
      <div class="mobile-header__mask" @click="mobileOpen = false"></div>
      <nav class="mobile-nav">
        <div class="nav-user-box">
          <div class="user-info">
            <div class="avatar-img">
              <span>{{ userInitial }}</span>
            </div>
            <div class="user-right">
              <b>{{ authStore.isAuthenticated ? displayName : '未登录' }}</b>
              <div class="text-xs line1">
                {{
                  authStore.isAuthenticated
                    ? `${authStore.pointsBalance} 积分可用`
                    : '登录后即可体验更多功能'
                }}
              </div>
            </div>
          </div>
          <div v-if="authStore.isAuthenticated" class="user-btn">
            <button type="button" class="btn l-vc-green" @click="goMobile('CreateResource')">
              <Plus />
              <span>发布资源</span>
            </button>
            <button
              v-if="authStore.isAuthenticated"
              type="button"
              class="btn l-vc-theme"
              @click="handleLogout"
            >
              <SwitchButton />
              <span>退出</span>
            </button>
          </div>
          <div v-else class="user-btn">
            <button type="button" class="btn l-vc-blue" @click="goMobile('Login')">
              <span>登录</span>
            </button>
            <button type="button" class="btn l-vc-green" @click="goMobile('Register')">
              <span>注册</span>
            </button>
          </div>
        </div>

        <ul>
          <li v-for="item in mobileNav" :key="item.routeName">
            <button
              type="button"
              class="aside-btn"
              :class="{ 'is-active': route.name === item.routeName }"
              @click="goMobile(item.routeName, item.query)"
            >
              <component :is="item.icon" class="nav-ico" />
              <span>{{ item.label }}</span>
            </button>
          </li>
        </ul>
      </nav>
    </div>

    <div class="search-modal" :class="{ show: searchOpen }">
      <div class="search-body">
        <form class="search-card" @submit.prevent="submitSearch">
          <div class="search-box">
            <input
              ref="searchInput"
              v-model.trim="keyword"
              type="search"
              placeholder="你想了解些什么"
            />
            <button type="submit" class="btn vc-theme" aria-label="搜索">
              <Search />
            </button>
          </div>
        </form>

        <div v-if="recentKeywords.length" class="search-keywords-box">
          <div class="search-card">
            <div class="text-muted search-keywords__head">
              <span>历史搜索</span>
              <button type="button" class="btn btn-sm" @click="clearRecentKeywords">清空</button>
            </div>
            <div class="search-keywords">
              <button
                v-for="item in recentKeywords"
                :key="item"
                type="button"
                class="btn btn-sm"
                @click="applyKeyword(item)"
              >
                {{ item }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch, type Component } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  ArrowDown,
  Clock,
  EditPen,
  Grid,
  House,
  Plus,
  Search,
  Star,
  SwitchButton,
  Trophy,
  User,
} from '@element-plus/icons-vue';
import { useAuthStore } from '../store/auth';

const RECENT_KEYWORD_KEY = 'recent_search_keywords';

const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();

const mobileOpen = ref(false);
const searchOpen = ref(false);
const scrolled = ref(false);
const keyword = ref('');
const searchInput = ref<HTMLInputElement | null>(null);
const recentKeywords = ref<string[]>([]);

const feedEntries = [
  { label: '最新资源流', value: 'latest', icon: Clock },
  { label: '热门资源流', value: 'hot', icon: Trophy },
  { label: '我关注的作者', value: 'following', icon: Star },
];

const mobileNav: { label: string; routeName: string; icon: Component; query: Record<string, string> }[] = [
  { label: '首页', routeName: 'Home', icon: House, query: {} },
  { label: '最新资源流', routeName: 'Resources', icon: Clock, query: {} },
  { label: '热门资源流', routeName: 'Resources', icon: Trophy, query: { feed: 'hot' } },
  { label: '我关注的作者', routeName: 'Resources', icon: Star, query: { feed: 'following' } },
  { label: '个人中心', routeName: 'Center', icon: User, query: {} },
];

const displayName = computed(() => authStore.currentUser?.username || '用户');
const userInitial = computed(() => (authStore.isAuthenticated ? displayName.value.slice(0, 1) : 'U'));

const loadRecentKeywords = () => {
  try {
    const stored = JSON.parse(localStorage.getItem(RECENT_KEYWORD_KEY) || '[]');
    recentKeywords.value = Array.isArray(stored) ? stored.slice(0, 8) : [];
  } catch {
    recentKeywords.value = [];
  }
};

const rememberKeyword = (value: string) => {
  if (!value) {
    return;
  }
  const next = [value, ...recentKeywords.value.filter((item) => item !== value)].slice(0, 8);
  recentKeywords.value = next;
  localStorage.setItem(RECENT_KEYWORD_KEY, JSON.stringify(next));
};

const clearRecentKeywords = () => {
  recentKeywords.value = [];
  localStorage.removeItem(RECENT_KEYWORD_KEY);
};

const goTo = (routeName: string) => {
  mobileOpen.value = false;
  router.push({ name: routeName });
};

const goToFeed = (feed: string) => {
  const query: Record<string, string> = feed === 'latest' ? {} : { feed };
  router.push({ name: 'Resources', query });
};

const goMobile = (routeName: string, query: Record<string, string> = {}) => {
  mobileOpen.value = false;
  router.push({ name: routeName, query });
};

const openSearch = async () => {
  searchOpen.value = true;
  await nextTick();
  searchInput.value?.focus();
};

const applyKeyword = (value: string) => {
  keyword.value = value;
  submitSearch();
};

const submitSearch = () => {
  const value = keyword.value.trim();
  searchOpen.value = false;
  rememberKeyword(value);
  router.push({ name: 'Resources', query: value ? { keyword: value, feed: 'latest' } : {} });
};

const handleLogout = async () => {
  mobileOpen.value = false;
  await authStore.logout();
  router.push({ name: 'Home' });
};

const onScroll = () => {
  scrolled.value = window.scrollY > 10;
};

const onKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') {
    searchOpen.value = false;
    mobileOpen.value = false;
  }
};

watch(
  () => route.fullPath,
  () => {
    mobileOpen.value = false;
    searchOpen.value = false;
  },
);

watch(
  () => route.query.keyword,
  (value) => {
    keyword.value = typeof value === 'string' ? value : '';
  },
  { immediate: true },
);

onMounted(() => {
  loadRecentKeywords();
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });
  window.addEventListener('keydown', onKeydown);
});

onUnmounted(() => {
  window.removeEventListener('scroll', onScroll);
  window.removeEventListener('keydown', onKeydown);
});
</script>

<style scoped>
.navbar-logo__btn {
  display: flex;
  align-items: center;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
}

.nav-ico {
  width: 15px;
  height: 15px;
  color: currentColor;
  opacity: 0.85;
}

.caret {
  width: 10px;
  height: 10px;
}

.search-body {
  display: flex;
  flex-direction: column;
}

.search-keywords-box {
  margin-top: 8px;
}

.search-keywords__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
}

.search-box .btn {
  font-size: 16px;
}
</style>

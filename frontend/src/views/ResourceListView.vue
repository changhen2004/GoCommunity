<template>
  <PageLayout>
    <template #aside>
      <li v-for="feed in feedOptions" :key="feed.value">
        <button
          type="button"
          class="aside-btn"
          :class="{ 'is-active': state.feed === feed.value }"
          @click="selectFeed(feed.value)"
        >
          <component :is="feed.icon" class="aside-ico" />
          <span>{{ feed.label }}</span>
        </button>
      </li>
      <li class="aside-divider"></li>
      <li>
        <button
          type="button"
          class="aside-btn"
          :class="{ 'is-active': !state.tag }"
          :disabled="state.feed === 'following'"
          @click="selectTag('')"
        >
          <PriceTag class="aside-ico" />
          <span>全部标签</span>
        </button>
      </li>
      <li v-for="tag in availableTags" :key="tag">
        <button
          type="button"
          class="aside-btn"
          :class="{ 'is-active': state.tag === tag }"
          :disabled="state.feed === 'following'"
          @click="selectTag(tag)"
        >
          <PriceTag class="aside-ico" />
          <span>#{{ tag }}</span>
        </button>
      </li>
      <li v-if="!availableTags.length" class="aside-empty">当前页没有标签样本</li>
    </template>

    <section class="content-card">
      <div class="content-card__head">
        <h4 class="tab-title is-active">
          <Grid class="tab-title__icon" />
          <span>{{ pageTitle }}</span>
        </h4>
        <div class="tab-to-more"></div>
        <span class="text-muted text-xs">
          第 {{ state.page }} 页 · 每页 {{ state.pageSize }} 条
        </span>
      </div>

      <div class="filter-bar">
        <form class="search-box filter-bar__search" @submit.prevent="applyFilters">
          <input
            v-model.trim="draftKeyword"
            type="search"
            :disabled="state.feed === 'following'"
            placeholder="搜索标题关键词"
          />
          <button
            type="submit"
            class="btn vc-theme"
            :disabled="state.feed === 'following'"
            aria-label="搜索"
          >
            <Search />
          </button>
        </form>

        <div class="filter-bar__tags">
          <button
            type="button"
            class="btn btn-sm"
            :class="{ active: state.tag === '' }"
            :disabled="state.feed === 'following'"
            @click="selectTag('')"
          >
            全部
          </button>
          <button
            v-for="tag in availableTags"
            :key="tag"
            type="button"
            class="btn btn-sm"
            :class="{ active: state.tag === tag }"
            :disabled="state.feed === 'following'"
            @click="selectTag(tag)"
          >
            {{ tag }}
          </button>
        </div>

        <div class="filter-bar__size">
          <button
            v-for="size in pageSizeOptions"
            :key="size"
            type="button"
            class="btn btn-sm"
            :class="{ active: state.pageSize === size }"
            @click="changePageSize(size)"
          >
            {{ size }} 条
          </button>
        </div>
      </div>
    </section>

    <section class="content-card">
      <div v-if="loading" class="card-grid card-grid--list">
        <div v-for="index in state.pageSize" :key="index" class="posts-item posts-item--list">
          <div class="item-media"></div>
          <div class="item-body">
            <el-skeleton :rows="3" animated />
          </div>
        </div>
      </div>

      <div v-else-if="errorMessage" class="empty-state">
        <p>{{ errorMessage }}</p>
        <button
          v-if="state.feed === 'following' && !authStore.isAuthenticated"
          type="button"
          class="btn vc-theme"
          @click="goToLogin"
        >
          去登录
        </button>
        <button v-else type="button" class="btn l-vc-theme" @click="fetchResources">
          重新加载
        </button>
      </div>

      <div v-else-if="!resources.length" class="empty-state">
        <p>当前筛选条件下暂无内容</p>
        <button type="button" class="btn l-vc-theme" @click="resetFilters">重置筛选</button>
      </div>

      <div v-else class="card-grid card-grid--list">
        <ResourceStoryCard
          v-for="resource in resources"
          :key="resource.id"
          :resource="resource"
          variant="list"
          @tag="selectTag"
        />
      </div>

      <div v-if="!loading && !errorMessage && resources.length" class="pagination">
        <button
          type="button"
          class="page-numbers"
          :disabled="state.page === 1"
          @click="changePage(state.page - 1)"
        >
          <ArrowLeft />
        </button>
        <span class="page-numbers is-current">{{ state.page }}</span>
        <button type="button" class="page-numbers" :disabled="!canGoNext" @click="changePage(state.page + 1)">
          <ArrowRight />
        </button>
      </div>
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ArrowLeft, ArrowRight, Clock, Grid, PriceTag, Search, Star, Trophy } from '@element-plus/icons-vue';
import PageLayout from '../components/PageLayout.vue';
import ResourceStoryCard from '../components/ResourceStoryCard.vue';
import { listArticles, listFollowingArticles } from '../api/article';
import { useAuthStore } from '../store/auth';
import type { FollowingFeedCursor, ResourceSummary } from '../types/resource';

type FeedMode = 'latest' | 'following' | 'hot';

interface CatalogState {
  page: number;
  pageSize: number;
  feed: FeedMode;
  keyword: string;
  tag: string;
}

const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();
const resources = ref<ResourceSummary[]>([]);
const loading = ref(false);
const errorMessage = ref('');
const draftKeyword = ref('');
const followingCursors = ref<Record<number, FollowingFeedCursor | undefined>>({ 1: undefined });
const state = reactive<CatalogState>({
  page: 1,
  pageSize: 6,
  feed: 'latest',
  keyword: '',
  tag: '',
});

const feedOptions = [
  { label: '最新资源流', value: 'latest' as FeedMode, icon: Clock },
  { label: '热门资源流', value: 'hot' as FeedMode, icon: Trophy },
  { label: '我关注的作者', value: 'following' as FeedMode, icon: Star },
];

const pageSizeOptions = [6, 12, 18];

const availableTags = computed(() => {
  const tagSet = new Set<string>();
  resources.value.forEach((resource) => {
    resource.tags?.forEach((tag) => {
      const normalized = tag.trim();
      if (normalized) {
        tagSet.add(normalized);
      }
    });
  });
  return Array.from(tagSet);
});

const pageTitle = computed(() => {
  if (state.feed === 'following') {
    return '我关注的作者';
  }
  if (state.keyword) {
    return `搜索：${state.keyword}`;
  }
  if (state.tag) {
    return `标签：${state.tag}`;
  }
  return state.feed === 'hot' ? '热门资源流' : '最新资源流';
});

const canGoNext = computed(() => {
  if (loading.value) {
    return false;
  }
  if (state.feed === 'following') {
    return !!followingCursors.value[state.page + 1];
  }
  return resources.value.length >= state.pageSize;
});

const parsePositiveNumber = (value: unknown, fallback: number) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
};

const normalizeQuery = (query: Record<string, unknown>): CatalogState => {
  let feed: FeedMode = 'latest';
  if (query.feed === 'following') {
    feed = 'following';
  } else if (query.feed === 'hot' || query.sort === 'hot') {
    feed = 'hot';
  }
  return {
    page: feed === 'following' ? 1 : parsePositiveNumber(query.page, 1),
    pageSize: [6, 12, 18].includes(Number(query.pageSize)) ? Number(query.pageSize) : 6,
    feed,
    keyword: feed === 'following' ? '' : typeof query.keyword === 'string' ? query.keyword : '',
    tag: feed === 'following' ? '' : typeof query.tag === 'string' ? query.tag : '',
  };
};

const buildRouteQuery = (nextState: CatalogState) => ({
  ...(nextState.page > 1 ? { page: String(nextState.page) } : {}),
  ...(nextState.pageSize !== 6 ? { pageSize: String(nextState.pageSize) } : {}),
  ...(nextState.feed !== 'latest' ? { feed: nextState.feed } : {}),
  ...(nextState.keyword ? { keyword: nextState.keyword } : {}),
  ...(nextState.tag ? { tag: nextState.tag } : {}),
});

const fetchResources = async () => {
  loading.value = true;
  errorMessage.value = '';

  try {
    if (state.feed === 'following') {
      if (!authStore.isAuthenticated) {
        resources.value = [];
        errorMessage.value = '请先登录后查看你关注作者发布的资源。';
        return;
      }

      const cursor = followingCursors.value[state.page];
      const response = await listFollowingArticles({
        pageSize: state.pageSize,
        beforeCreatedAt: cursor?.beforeCreatedAt,
        beforeId: cursor?.beforeId,
      });
      resources.value = response.items;
      followingCursors.value[state.page + 1] = response.nextCursor;
      return;
    }

    resources.value = await listArticles({
      page: state.page,
      pageSize: state.pageSize,
      sort: state.feed === 'hot' ? 'hot' : 'latest',
      keyword: state.keyword || undefined,
      tag: state.tag || undefined,
    });
  } catch (error) {
    console.error('Failed to load resources:', error);
    errorMessage.value = '资源列表加载失败，请稍后重试。';
  } finally {
    loading.value = false;
  }
};

const navigateWithQuery = (patch: Partial<CatalogState>) => {
  const nextState: CatalogState = { ...state, ...patch };
  router.replace({ name: 'Resources', query: buildRouteQuery(nextState) });
};

const applyFilters = () => {
  if (state.feed === 'following') {
    return;
  }
  navigateWithQuery({ page: 1, keyword: draftKeyword.value.trim() });
};

const resetFilters = () => {
  draftKeyword.value = '';
  followingCursors.value = { 1: undefined };
  router.replace({ name: 'Resources', query: {} });
};

const selectTag = (tag: string) => {
  if (state.feed === 'following') {
    return;
  }
  navigateWithQuery({ page: 1, tag });
};

const selectFeed = (feed: FeedMode) => {
  followingCursors.value = { 1: undefined };
  state.feed = feed;
  navigateWithQuery({
    page: 1,
    feed,
    keyword: feed === 'following' ? '' : state.keyword,
    tag: feed === 'following' ? '' : state.tag,
  });
};

const changePageSize = (size: number) => {
  followingCursors.value = { 1: undefined };
  navigateWithQuery({ page: 1, pageSize: size });
};

const changePage = (page: number) => {
  if (page < 1 || loading.value) {
    return;
  }
  if (state.feed === 'following' && page > state.page && !followingCursors.value[page]) {
    return;
  }
  navigateWithQuery({ page });
};

const goToLogin = () => {
  router.push({ name: 'Login' });
};

watch(
  () => route.query,
  async (query) => {
    const normalized = normalizeQuery(query as Record<string, unknown>);
    state.page = normalized.page;
    state.pageSize = normalized.pageSize;
    state.feed = normalized.feed;
    state.keyword = normalized.keyword;
    state.tag = normalized.tag;
    draftKeyword.value = normalized.keyword;
    await fetchResources();
  },
  { immediate: true },
);
</script>

<style scoped>
.aside-ico {
  width: 15px;
  height: 15px;
  color: var(--theme-color);
}

.aside-divider {
  height: 1px;
  margin: 6px 0;
  background: var(--muted-bg-color);
  list-style: none;
}

.aside-empty {
  padding: 8px 9px;
  color: var(--muted-color);
  font-size: 13px;
  list-style: none;
}

.aside-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
}

.filter-bar__search {
  flex: 1 1 240px;
  max-width: 340px;
}

.filter-bar__tags,
.filter-bar__size {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.filter-bar__size {
  margin-left: auto;
}

.card-grid--list {
  grid-template-columns: repeat(1, minmax(0, 1fr));
}

@media (min-width: 992px) {
  .card-grid--list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.posts-item--list .item-body :deep(.el-skeleton) {
  --el-skeleton-color: var(--muted-bg-color);
  --el-skeleton-to-color: var(--muted-bg-color-l);
}

.page-numbers :deep(svg),
.page-numbers svg {
  width: 14px;
  height: 14px;
}
</style>

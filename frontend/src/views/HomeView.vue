<template>
  <PageLayout>
    <template #aside>
      <li v-for="item in anchorLinks" :key="item.href">
        <a class="aside-btn" :href="item.href">
          <component :is="item.icon" class="aside-ico" />
          <span>{{ item.label }}</span>
        </a>
      </li>
      <li class="aside-divider"></li>
      <li>
        <button type="button" class="aside-btn" @click="openResources({})">
          <Grid class="aside-ico" />
          <span>全部资源</span>
        </button>
      </li>
      <li>
        <button type="button" class="aside-btn" @click="openResources({ feed: 'following' })">
          <Star class="aside-ico" />
          <span>我关注的作者</span>
        </button>
      </li>
      <li>
        <button
          type="button"
          class="aside-btn"
          :disabled="!authStore.isAuthenticated"
          @click="router.push({ name: 'CreateResource' })"
        >
          <Plus class="aside-ico" />
          <span>发布资源</span>
        </button>
      </li>
    </template>

    <section class="content-card">
      <div class="content-card__head">
        <h4 class="tab-title">
          <Trophy class="tab-title__icon" />
          <span>热门资源</span>
        </h4>
        <div class="tab-to-more"></div>
        <button type="button" class="btn-more" @click="openResources({ feed: 'hot' })">
          更多内容
        </button>
      </div>

      <div v-if="loading" class="card-grid">
        <div v-for="index in 6" :key="index" class="posts-item posts-item--card">
          <div class="item-media"></div>
          <div class="item-body">
            <el-skeleton :rows="3" animated />
          </div>
        </div>
      </div>
      <div v-else-if="errorMessage && !hotResources.length" class="empty-state">
        <p>{{ errorMessage }}</p>
        <button type="button" class="btn l-vc-theme" @click="fetchHomeFeed">重新加载</button>
      </div>
      <div v-else-if="hotResources.length" class="card-grid">
        <ResourceStoryCard
          v-for="resource in hotResources.slice(0, 6)"
          :key="resource.id"
          :resource="resource"
          variant="card"
          :show-preview="false"
          @tag="handleTagClick"
        />
      </div>
      <div v-else class="empty-state">暂时没有热门资源</div>
    </section>

    <section id="latest" class="content-card">
      <div class="content-card__head">
        <h4 class="tab-title">
          <Clock class="tab-title__icon" />
          <span>最新上架</span>
        </h4>
        <div class="tab-to-more"></div>
        <button type="button" class="btn-more" @click="openResources({})">更多内容</button>
      </div>

      <div v-if="loading" class="card-grid">
        <div v-for="index in 6" :key="index" class="posts-item posts-item--card">
          <div class="item-media"></div>
          <div class="item-body">
            <el-skeleton :rows="3" animated />
          </div>
        </div>
      </div>
      <div v-else-if="latestResources.length" class="card-grid">
        <ResourceStoryCard
          v-for="resource in latestResources.slice(0, 6)"
          :key="resource.id"
          :resource="resource"
          variant="card"
          :show-preview="false"
          @tag="handleTagClick"
        />
      </div>
      <div v-else class="empty-state">暂时没有最新资源</div>
    </section>

    <section id="free" class="content-card">
      <div class="content-card__head">
        <h4 class="tab-title">
          <Present class="tab-title__icon" />
          <span>免费可读</span>
        </h4>
        <div class="tab-to-more"></div>
        <button type="button" class="btn-more" @click="openResources({})">浏览更多</button>
      </div>

      <div v-if="loading" class="card-grid">
        <div v-for="index in 6" :key="index" class="posts-item posts-item--card">
          <div class="item-media"></div>
          <div class="item-body">
            <el-skeleton :rows="3" animated />
          </div>
        </div>
      </div>
      <div v-else-if="freeResources.length" class="card-grid">
        <ResourceStoryCard
          v-for="resource in freeResources"
          :key="resource.id"
          :resource="resource"
          variant="card"
          :show-preview="false"
          @tag="handleTagClick"
        />
      </div>
      <div v-else class="empty-state">当前样本里还没有免费资源</div>
    </section>

    <section id="tags" class="content-card">
      <div class="content-card__head">
        <h4 class="tab-title">
          <PriceTag class="tab-title__icon" />
          <span>标签分区</span>
        </h4>
        <div class="tab-to-more"></div>
        <button type="button" class="btn-more" @click="openResources({})">全部标签</button>
      </div>

      <div v-if="topTags.length" class="tag-cloud">
        <button
          v-for="tag in topTags"
          :key="tag.name"
          type="button"
          class="badge tag-cloud__item"
          @click="openResources({ tag: tag.name })"
        >
          <strong>{{ tag.name }}</strong>
          <span>{{ tag.count }}</span>
        </button>
      </div>
      <div v-else class="empty-state">当前资源样本还没有形成明显的标签分布</div>
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { Clock, Grid, Plus, Present, PriceTag, Star, Trophy } from '@element-plus/icons-vue';
import PageLayout from '../components/PageLayout.vue';
import ResourceStoryCard from '../components/ResourceStoryCard.vue';
import { listArticles } from '../api/article';
import { useAuthStore } from '../store/auth';
import type { ResourceSummary } from '../types/resource';

interface TagStat {
  name: string;
  count: number;
}

const anchorLinks = [
  { href: '#hot', label: '热门资源', icon: Trophy },
  { href: '#latest', label: '最新上架', icon: Clock },
  { href: '#free', label: '免费可读', icon: Present },
  { href: '#tags', label: '标签分区', icon: PriceTag },
];

const router = useRouter();
const authStore = useAuthStore();
const loading = ref(false);
const errorMessage = ref('');
const latestResources = ref<ResourceSummary[]>([]);
const hotResources = ref<ResourceSummary[]>([]);

const uniqueResources = computed(() => {
  const orderedMap = new Map<number, ResourceSummary>();
  [...hotResources.value, ...latestResources.value].forEach((resource) => {
    if (!orderedMap.has(resource.id)) {
      orderedMap.set(resource.id, resource);
    }
  });
  return Array.from(orderedMap.values());
});

const freeResources = computed(() =>
  uniqueResources.value.filter((resource) => resource.isFree).slice(0, 6),
);

const topTags = computed<TagStat[]>(() => {
  const tagMap = new Map<string, number>();
  uniqueResources.value.forEach((resource) => {
    resource.tags?.forEach((tag) => {
      const normalized = tag.trim();
      if (!normalized) {
        return;
      }
      tagMap.set(normalized, (tagMap.get(normalized) || 0) + 1);
    });
  });

  return Array.from(tagMap.entries())
    .map(([name, count]) => ({ name, count }))
    .sort((left, right) => right.count - left.count)
    .slice(0, 12);
});

const openResources = (query: Record<string, string>) => {
  router.push({ name: 'Resources', query });
};

const handleTagClick = (tag: string) => {
  openResources({ tag });
};

const fetchHomeFeed = async () => {
  loading.value = true;
  errorMessage.value = '';

  try {
    const [latest, hot] = await Promise.all([
      listArticles({ page: 1, pageSize: 8, sort: 'latest' }),
      listArticles({ page: 1, pageSize: 8, sort: 'hot' }),
    ]);
    latestResources.value = latest;
    hotResources.value = hot;
  } catch (error) {
    console.error('Failed to load home feed:', error);
    errorMessage.value = '首页资源流暂时不可用，请稍后重试。';
  } finally {
    loading.value = false;
  }
};

onMounted(fetchHomeFeed);
</script>

<style scoped>
#hot,
#latest,
#free,
#tags {
  scroll-margin-top: calc(var(--main-nav-hight) + 10px);
}

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

.aside-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.tag-cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-cloud__item {
  gap: 6px;
  padding: 6px 10px;
  font-size: 13px;
}

.tag-cloud__item strong {
  font-weight: 600;
}

.tag-cloud__item span {
  opacity: 0.7;
}

.posts-item--card .item-body :deep(.el-skeleton) {
  --el-skeleton-color: var(--muted-bg-color);
  --el-skeleton-to-color: var(--muted-bg-color-l);
}
</style>

<template>
  <PageLayout>
    <template #aside>
      <li class="aside-block">
        <div class="user-info">
          <div class="avatar-img">
            <span>{{ authStore.currentUser?.username?.slice(0, 1) || 'U' }}</span>
          </div>
          <div class="user-right">
            <b class="line1">{{ authStore.currentUser?.username || '未登录用户' }}</b>
            <span class="text-muted text-xs">用户中心</span>
          </div>
        </div>
      </li>

      <li class="aside-block">
        <button
          type="button"
          class="btn vc-theme btn-block"
          :disabled="checkInLoading || checkInCompleted || !authStore.isAuthenticated"
          @click="handleCheckIn"
        >
          <Calendar />
          <span>{{ checkInLoading ? '签到中…' : checkInCompleted ? '今日已签到' : '立即签到' }}</span>
        </button>
        <p class="check-in-hint text-muted text-xs">
          {{ checkInCompleted ? '今日签到已完成，可明天再来。' : '每日签到可获得积分奖励。' }}
        </p>
      </li>

      <li class="aside-block">
        <p class="aside-title">
          <Histogram class="tab-title__icon" />
          <span>数据概览</span>
        </p>
        <div class="stat-grid">
          <div class="stat-item">
            <span>我的积分</span>
            <strong>{{ authStore.pointsBalance }}</strong>
          </div>
          <div class="stat-item">
            <span>我的收藏</span>
            <strong>{{ favoriteArticles.length }}</strong>
          </div>
          <div class="stat-item">
            <span>积分权益</span>
            <strong>{{ authStore.pointsSummary?.privileges?.length ?? 0 }}</strong>
          </div>
        </div>
      </li>
    </template>

    <section v-if="!authStore.isAuthenticated" class="content-card">
      <el-empty description="登录后可查看用户中心">
        <el-button type="primary" @click="goToLogin">前往登录</el-button>
      </el-empty>
    </section>

    <template v-else>
      <section class="content-card">
        <div class="content-card__head">
          <h4 class="tab-title is-active">
            <Collection class="tab-title__icon" />
            <span>我的收藏</span>
          </h4>
          <div class="tab-to-more"></div>
          <button type="button" class="btn-more" @click="loadFavorites">刷新</button>
        </div>

        <div v-if="favoriteLoading" class="content-loading">
          <el-skeleton :rows="4" animated />
        </div>
        <div v-else-if="favoriteError" class="empty-state">
          <p>收藏加载失败</p>
          <p class="text-muted text-xs">{{ favoriteError }}</p>
          <button type="button" class="btn l-vc-theme" @click="loadFavorites">重试</button>
        </div>
        <div v-else-if="!favoriteArticles.length" class="empty-state">你还没有收藏任何资源</div>
        <div v-else class="favorite-list">
          <button
            v-for="favorite in favoriteArticles"
            :key="favorite.id"
            class="favorite-item"
            type="button"
            @click="goToArticle(favorite.id)"
          >
            <div class="favorite-cover">
              <img
                v-if="favorite.coverUrl"
                :src="favorite.coverUrl"
                :alt="favorite.title"
                loading="lazy"
                decoding="async"
              />
              <div v-else class="item-media__placeholder">{{ favorite.title.slice(0, 1) }}</div>
            </div>
            <div class="favorite-body">
              <h3 class="favorite-title line1">{{ favorite.title }}</h3>
              <p class="favorite-preview line2">{{ favorite.preview }}</p>
              <div class="favorite-meta item-meta">
                <span class="badge" :class="favorite.isFree ? 'l-vc-green' : 'l-vc-theme'">
                  {{ favorite.isFree ? '免费' : `${favorite.requiredPoints} 积分` }}
                </span>
                <span>{{ favorite.author.username }}</span>
              </div>
            </div>
          </button>
        </div>
      </section>

      <section class="content-card">
        <div class="content-card__head">
          <h4 class="tab-title is-active">
            <Coin class="tab-title__icon" />
            <span>我的积分</span>
          </h4>
          <div class="tab-to-more"></div>
          <button type="button" class="btn-more" @click="loadPointRecords">刷新</button>
        </div>

        <div v-if="authStore.pointsSummary" class="points-summary">
          <div class="points-balance">
            <span class="text-muted text-xs">当前余额</span>
            <strong>{{ authStore.pointsBalance }}</strong>
          </div>

          <div class="redeem-box">
            <div>
              <span class="privilege-label">
                <Present />
                <span>权益兑换</span>
              </span>
              <p class="redeem-copy text-muted text-xs">
                使用积分兑换额外展示权益，兑换后会显示在下方列表中。
              </p>
            </div>
            <div class="redeem-actions">
              <el-select v-model="selectedPrivilegeKey" class="redeem-select" placeholder="选择权益">
                <el-option
                  v-for="option in privilegeOptions"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
              <el-button
                type="primary"
                :loading="redeemLoading"
                :disabled="!selectedPrivilegeKey"
                @click="handleRedeemPrivilege"
              >
                立即兑换
              </el-button>
            </div>
          </div>

          <div class="privilege-list">
            <span class="privilege-label">已兑换权益</span>
            <el-tag
              v-for="privilege in authStore.pointsSummary.privileges"
              :key="privilege.privilegeKey"
              effect="plain"
              type="success"
            >
              {{ privilege.privilegeKey }}
            </el-tag>
            <span v-if="!authStore.pointsSummary.privileges.length" class="text-muted text-xs">
              暂无权益
            </span>
          </div>
        </div>

        <div v-if="recordLoading" class="content-loading">
          <el-skeleton :rows="4" animated />
        </div>
        <div v-else-if="recordError" class="empty-state">
          <p>积分记录加载失败</p>
          <p class="text-muted text-xs">{{ recordError }}</p>
          <button type="button" class="btn l-vc-theme" @click="loadPointRecords">重试</button>
        </div>
        <div v-else-if="!pointRecords.length" class="empty-state">暂无积分记录</div>
        <div v-else class="record-list">
          <div v-for="record in pointRecords" :key="record.id" class="record-item">
            <div class="record-main">
              <h3 class="line1">{{ record.description || record.source }}</h3>
              <p class="text-muted text-xs">{{ formatDate(record.createdAt) }}</p>
            </div>
            <div class="record-side">
              <strong :class="record.change >= 0 ? 'record-positive' : 'record-negative'">
                {{ record.change >= 0 ? `+${record.change}` : record.change }}
              </strong>
              <span class="text-muted text-xs">余额 {{ record.balanceAfter }}</span>
            </div>
          </div>
        </div>
      </section>
    </template>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { ElMessage } from 'element-plus';
import { Calendar, Coin, Collection, Histogram, Present } from '@element-plus/icons-vue';
import PageLayout from '../components/PageLayout.vue';
import { listMyFavorites, type FavoriteArticle } from '../api/favorite';
import { checkIn } from '../api/checkin';
import { getMyPointsRecords, redeemPrivilege, type PointsRecord } from '../api/points';
import { useAuthStore } from '../store/auth';

const router = useRouter();
const authStore = useAuthStore();

const favoriteArticles = ref<FavoriteArticle[]>([]);
const pointRecords = ref<PointsRecord[]>([]);
const favoriteLoading = ref(false);
const recordLoading = ref(false);
const checkInLoading = ref(false);
const favoriteError = ref('');
const recordError = ref('');
const checkInDate = ref<string | null>(localStorage.getItem('daily_check_in_date'));
const redeemLoading = ref(false);
const selectedPrivilegeKey = ref('');

const today = new Date().toISOString().slice(0, 10);

const checkInCompleted = computed(() => checkInDate.value === today);
const privilegeOptions = [
  { label: '精选推荐位 - 30 积分', value: 'feature_article' },
  { label: '优先发布 - 50 积分', value: 'priority_publish' },
];

const loadFavorites = async () => {
  if (!authStore.isAuthenticated) {
    favoriteArticles.value = [];
    return;
  }

  favoriteLoading.value = true;
  favoriteError.value = '';

  try {
    favoriteArticles.value = await listMyFavorites();
  } catch (error) {
    console.error('Failed to load favorites:', error);
    favoriteError.value = '请稍后重试。';
  } finally {
    favoriteLoading.value = false;
  }
};

const loadPointRecords = async () => {
  if (!authStore.isAuthenticated) {
    pointRecords.value = [];
    return;
  }

  recordLoading.value = true;
  recordError.value = '';

  try {
    pointRecords.value = await getMyPointsRecords();
  } catch (error) {
    console.error('Failed to load point records:', error);
    recordError.value = '请稍后重试。';
  } finally {
    recordLoading.value = false;
  }
};

const handleCheckIn = async () => {
  if (!authStore.isAuthenticated || checkInCompleted.value) {
    return;
  }

  checkInLoading.value = true;
  try {
    const response = await checkIn();
    checkInDate.value = today;
    localStorage.setItem('daily_check_in_date', today);
    await Promise.all([authStore.refreshSummary(), loadPointRecords()]);
    ElMessage.success(response.message || '签到成功');
  } catch (error) {
    console.error('Check-in failed:', error);
    ElMessage.error('签到失败，请稍后再试。');
  } finally {
    checkInLoading.value = false;
  }
};

const handleRedeemPrivilege = async () => {
  if (!authStore.isAuthenticated || !selectedPrivilegeKey.value) {
    return;
  }

  redeemLoading.value = true;
  try {
    const response = await redeemPrivilege({ privilegeKey: selectedPrivilegeKey.value });
    await Promise.all([authStore.refreshSummary(), loadPointRecords()]);
    selectedPrivilegeKey.value = '';
    ElMessage.success(response.message || '权益兑换成功');
  } catch (error) {
    console.error('Failed to redeem privilege:', error);
    ElMessage.error('权益兑换失败，请检查积分余额或是否已兑换。');
  } finally {
    redeemLoading.value = false;
  }
};

const goToLogin = () => {
  router.push({ name: 'Login' });
};

const goToArticle = (id: number) => {
  router.push({ name: 'ResourceDetail', params: { id } });
};

const formatDate = (value: string) => {
  return new Date(value).toLocaleString('zh-CN', {
    hour12: false,
  });
};

onMounted(async () => {
  if (!authStore.isAuthenticated) {
    return;
  }

  await Promise.all([loadFavorites(), loadPointRecords()]);
});
</script>

<style scoped>
/* theme.css ships no `.content-card` skin, so each page paints its own. */
.content-card {
  padding: var(--home-card-padding);
  border-radius: var(--main-radius);
  background: var(--main-bg-color);
  transition: box-shadow 0.3s, background-color 0.3s;
}

.content-card:hover {
  box-shadow: 0 20px 25px -10px var(--main-shadow);
}

/* ------------------------------------------------------------- aside rail */
.aside-block:first-child {
  margin-bottom: 4px;
}

.check-in-hint {
  margin: 8px 0 0;
  line-height: 1.5;
}

.stat-grid {
  grid-template-columns: minmax(0, 1fr);
}

.stat-item {
  flex-direction: row;
  align-items: baseline;
  justify-content: space-between;
}

/* ---------------------------------------------------------- shared rows */
.content-loading :deep(.el-skeleton) {
  --el-skeleton-color: var(--muted-bg-color);
  --el-skeleton-to-color: var(--muted-bg-color-l);
}

.favorite-list,
.record-list {
  display: flex;
  flex-direction: column;
}

.favorite-item,
.record-item {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  width: 100%;
  padding: 12px 0;
  border-bottom: 1px solid var(--muted-bg-color);
}

.favorite-item {
  border-top: 0;
  border-right: 0;
  border-left: 0;
  background: none;
  text-align: left;
  cursor: pointer;
}

.favorite-item:last-child,
.record-item:last-child {
  padding-bottom: 0;
  border-bottom: 0;
}

/* ------------------------------------------------------------ favorites */
.favorite-cover {
  position: relative;
  flex: 0 0 120px;
  width: 120px;
  height: 86px;
  overflow: hidden;
  border-radius: var(--theme-border-radius-md);
  background: var(--muted-bg-a-color);
}

.favorite-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.favorite-cover .item-media__placeholder {
  font-size: 28px;
}

.favorite-body {
  min-width: 0;
  flex: 1 1 auto;
}

.favorite-title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 500;
  transition: color 0.3s;
}

.favorite-item:hover .favorite-title {
  color: var(--theme-color);
}

.favorite-preview {
  margin: 0 0 8px;
  color: var(--muted-color2);
  font-size: 13px;
  line-height: 1.6;
}

.favorite-meta {
  justify-content: space-between;
}

/* --------------------------------------------------------------- points */
.points-summary {
  padding: 12px;
  margin-bottom: 12px;
  border-radius: var(--theme-border-radius-md);
  background: var(--muted-bg-a-color);
}

.points-balance strong {
  display: block;
  margin-top: 4px;
  font-size: 26px;
}

.redeem-box {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.redeem-copy {
  margin: 4px 0 0;
  line-height: 1.6;
}

.redeem-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.redeem-select {
  min-width: 200px;
}

.privilege-label {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  color: var(--muted-color);
  font-size: 12px;
}

.privilege-label svg {
  width: 14px;
  height: 14px;
  color: var(--theme-color);
}

.privilege-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-top: 12px;
}

.record-item {
  align-items: center;
  justify-content: space-between;
}

.record-main {
  min-width: 0;
}

.record-main h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 500;
}

.record-main p {
  margin: 4px 0 0;
}

.record-side {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: flex-end;
  text-align: right;
}

.record-side strong {
  font-size: 15px;
}

.record-positive {
  color: var(--theme-color);
}

.record-negative {
  color: var(--muted-color2);
}

@media (max-width: 767px) {
  .favorite-item {
    flex-direction: column;
  }

  .favorite-cover {
    flex: none;
    width: 100%;
    height: 160px;
  }
}
</style>

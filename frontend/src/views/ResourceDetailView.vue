<template>
  <PageLayout>
    <template v-if="resource" #aside>
      <li class="aside-block">
        <h5 class="aside-title">
          <Lock class="tab-title__icon" />
          <span>访问规则</span>
        </h5>
        <p class="gate-status" :class="gateState.className">{{ gateState.label }}</p>
        <p class="gate-copy">{{ gateState.description }}</p>

        <div class="stat-item points-item">
          <span>当前积分</span>
          <strong>{{ authStore.pointsBalance }}</strong>
        </div>

        <button
          v-if="shouldShowUnlockButton"
          type="button"
          class="btn vc-theme btn-block widget-btn"
          :disabled="unlocking"
          @click="handleUnlock"
        >
          <Unlock />
          <span>{{ unlocking ? '解锁中…' : `使用 ${resource.requiredPoints || 0} 积分解锁` }}</span>
        </button>

        <button
          v-else-if="!authStore.isAuthenticated"
          type="button"
          class="btn l-vc-theme btn-block widget-btn"
          @click="redirectToLogin"
        >
          <SwitchButton />
          <span>登录后查看权限</span>
        </button>
      </li>

      <li class="aside-block">
        <h5 class="aside-title">
          <Histogram class="tab-title__icon" />
          <span>互动统计</span>
        </h5>
        <div class="stat-grid">
          <div class="stat-item">
            <span>点赞</span>
            <strong>{{ likes }}</strong>
          </div>
          <div class="stat-item">
            <span>浏览</span>
            <strong>{{ resource.stats?.viewCount ?? resource.viewCount ?? 0 }}</strong>
          </div>
          <div class="stat-item">
            <span>评论</span>
            <strong>{{ resource.stats?.commentCount ?? resource.commentCount ?? 0 }}</strong>
          </div>
          <div class="stat-item">
            <span>收藏</span>
            <strong>{{ resource.stats?.favoriteCount ?? resource.favoriteCount ?? 0 }}</strong>
          </div>
        </div>

        <div class="widget-actions">
          <button type="button" class="btn l-vc-theme btn-block" @click="handleLikeResource">
            <Star />
            <span>点赞资源</span>
          </button>
          <button
            type="button"
            class="btn btn-block"
            :class="isFavorited ? 'vc-red' : 'j-vc-green'"
            :disabled="favoriteSubmitting"
            @click="handleFavoriteToggle"
          >
            <Collection />
            <span>{{ isFavorited ? '取消收藏' : '收藏资源' }}</span>
          </button>
        </div>
      </li>

      <li v-if="resource.tags?.length" class="aside-block">
        <h5 class="aside-title">
          <PriceTag class="tab-title__icon" />
          <span>标签入口</span>
        </h5>
        <button
          v-for="tag in resource.tags"
          :key="tag"
          type="button"
          class="aside-btn"
          @click="goToTag(tag)"
        >
          <PriceTag class="aside-ico" />
          <span class="line1">#{{ tag }}</span>
        </button>
      </li>
    </template>

    <section v-if="loading" class="content-card">
      <el-skeleton animated>
        <template #template>
          <el-skeleton-item variant="image" style="width: 100%; height: 300px" />
          <div class="skeleton-stack">
            <el-skeleton-item variant="h1" style="width: 54%" />
            <el-skeleton-item variant="text" style="width: 92%" />
            <el-skeleton-item variant="text" style="width: 88%" />
            <el-skeleton-item variant="text" style="width: 76%" />
          </div>
        </template>
      </el-skeleton>
    </section>

    <section v-else-if="errorMessage" class="content-card">
      <el-result icon="warning" title="资源加载失败" :sub-title="errorMessage">
        <template #extra>
          <button type="button" class="btn vc-theme" @click="fetchPageData">重新加载</button>
        </template>
      </el-result>
    </section>

    <template v-else-if="resource">
      <section class="content-card detail-hero">
        <div class="item-media detail-cover">
          <img
            v-if="resource.coverUrl"
            :src="resource.coverUrl"
            :alt="resource.title"
            decoding="async"
          />
          <div v-else class="item-media__placeholder">{{ resource.title.slice(0, 1) }}</div>
        </div>

        <div class="detail-hero__body">
          <div class="detail-hero__bar">
            <button type="button" class="btn btn-sm" @click="goBackToList">
              <ArrowLeft />
              <span>返回资源广场</span>
            </button>
            <div class="detail-chips">
              <span class="badge">{{ resource.status || 'published' }}</span>
              <span class="badge" :class="resource.isFree ? 'j-vc-green' : 'j-vc-yellow'">
                {{ resource.isFree ? '免费资源' : `${resource.requiredPoints || 0} 积分解锁` }}
              </span>
            </div>
          </div>

          <h1 class="detail-title">{{ resource.title }}</h1>
          <p class="detail-preview">{{ resource.preview }}</p>

          <div class="item-meta detail-meta">
            <span>
              <User />
              {{ resource.author?.username || '匿名作者' }}
            </span>
            <span>
              <Lock />
              {{ resource.isFree ? '公开阅读' : '积分门槛' }}
            </span>
            <span>
              <Star />
              {{ likes }}
            </span>
          </div>

          <div v-if="resource.tags?.length" class="detail-tags">
            <button
              v-for="tag in resource.tags"
              :key="tag"
              type="button"
              class="badge l-vc-theme"
              @click="goToTag(tag)"
            >
              #{{ tag }}
            </button>
          </div>
        </div>
      </section>

      <section class="content-card">
        <div class="author-row">
          <div class="author-avatar">{{ resource.author?.username?.slice(0, 1) || 'U' }}</div>
          <div class="author-copy">
            <strong>{{ resource.author?.username || '匿名作者' }}</strong>
            <span class="text-muted text-xs">
              作者 ID #{{ resource.author?.id || resource.authorId }}
            </span>
          </div>
          <button
            v-if="canShowFollowButton"
            type="button"
            class="btn btn-sm"
            :class="authorSocialStatus?.isFollowing ? '' : 'vc-theme'"
            :disabled="followSubmitting"
            @click="handleFollowToggle"
          >
            <Star />
            <span>{{ authorSocialStatus?.isFollowing ? '取消关注' : '关注作者' }}</span>
          </button>
        </div>

        <div class="stat-grid summary-stats">
          <div class="stat-item">
            <span>浏览</span>
            <strong>{{ resource.stats?.viewCount ?? resource.viewCount ?? 0 }}</strong>
          </div>
          <div class="stat-item">
            <span>评论</span>
            <strong>{{ resource.stats?.commentCount ?? resource.commentCount ?? 0 }}</strong>
          </div>
          <div class="stat-item">
            <span>粉丝</span>
            <strong>{{ authorSocialStatus?.followerCount ?? 0 }}</strong>
          </div>
          <div class="stat-item">
            <span>关注</span>
            <strong>{{ authorSocialStatus?.followingCount ?? 0 }}</strong>
          </div>
        </div>
      </section>

      <section class="content-card">
        <div class="content-card__head">
          <h4 class="tab-title">
            <Reading class="tab-title__icon" />
            <span>资源正文</span>
          </h4>
          <div class="tab-to-more"></div>
          <button
            v-if="resource.tags?.[0]"
            type="button"
            class="btn-more"
            @click="goToTag(resource.tags[0])"
          >
            查看同标签内容
          </button>
        </div>

        <p class="body-copy">{{ resource.content }}</p>

        <div v-if="resource.contentImages?.length" class="content-gallery">
          <img
            v-for="(image, index) in resource.contentImages"
            :key="`${image}-${index}`"
            :src="image"
            :alt="`${resource.title} 配图 ${index + 1}`"
            loading="lazy"
            decoding="async"
          />
        </div>
      </section>

      <section v-if="relatedResources.length" class="content-card">
        <div class="content-card__head">
          <h4 class="tab-title">
            <CollectionTag class="tab-title__icon" />
            <span>相关资源</span>
          </h4>
          <div class="tab-to-more"></div>
          <button type="button" class="btn-more" @click="goBackToList">浏览更多</button>
        </div>

        <div class="card-grid related-grid">
          <ResourceStoryCard
            v-for="item in relatedResources"
            :key="item.id"
            :resource="item"
            variant="card"
            @tag="goToTag"
          />
        </div>
      </section>

      <section class="content-card">
        <div class="content-card__head">
          <h4 class="tab-title">
            <ChatDotRound class="tab-title__icon" />
            <span>评论区</span>
          </h4>
          <div class="tab-to-more"></div>
          <button type="button" class="btn-more" :disabled="commentLoading" @click="fetchComments">
            刷新
          </button>
        </div>

        <div v-if="authStore.isAuthenticated" class="comment-editor">
          <textarea
            v-model="commentForm"
            class="form-control"
            rows="4"
            maxlength="1000"
            placeholder="写下你的评论，分享你对这个资源的看法。"
          ></textarea>
          <div class="comment-editor__actions">
            <span class="text-muted text-xs">
              {{ commentForm.length }}/1000 · 登录用户可以参与讨论并推动内容热度上升。
            </span>
            <button
              type="button"
              class="btn vc-theme"
              :disabled="commentSubmitting || !commentForm.trim()"
              @click="handleCreateComment"
            >
              <ChatDotRound />
              <span>{{ commentSubmitting ? '发布中…' : '发布评论' }}</span>
            </button>
          </div>
        </div>
        <div v-else class="comment-guest">
          <span>登录后可以发表评论并参与互动</span>
          <button type="button" class="btn btn-sm l-vc-theme" @click="redirectToLogin">
            <SwitchButton />
            <span>去登录</span>
          </button>
        </div>

        <div v-if="commentLoading" class="comment-loading">
          <el-skeleton :rows="3" animated />
        </div>
        <el-result
          v-else-if="commentErrorMessage"
          icon="warning"
          title="评论加载失败"
          :sub-title="commentErrorMessage"
        >
          <template #extra>
            <button type="button" class="btn vc-theme" @click="fetchComments">重试</button>
          </template>
        </el-result>
        <el-empty v-else-if="!comments.length" description="还没有评论，来发表第一条观点" />
        <div v-else class="comment-list">
          <article v-for="comment in comments" :key="comment.id" class="comment-item">
            <div class="comment-item__head">
              <div>
                <strong>{{ comment.author.username }}</strong>
                <span class="comment-item__time text-muted text-xs">
                  {{ formatDate(comment.createdAt) }}
                </span>
              </div>
              <button
                v-if="canDeleteComment(comment.userId)"
                type="button"
                class="btn btn-sm vc-red"
                :disabled="deletingCommentId === comment.id"
                @click="handleDeleteComment(comment.id)"
              >
                <Delete />
                <span>{{ deletingCommentId === comment.id ? '删除中' : '删除' }}</span>
              </button>
            </div>
            <p class="comment-item__content">{{ comment.content }}</p>
          </article>
        </div>
      </section>
    </template>

    <section v-else class="content-card">
      <el-empty description="资源不存在或暂时不可用" />
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ElMessage } from 'element-plus';
import {
  ArrowLeft,
  ChatDotRound,
  Collection,
  CollectionTag,
  Delete,
  Histogram,
  Lock,
  PriceTag,
  Reading,
  Star,
  SwitchButton,
  Unlock,
  User,
} from '@element-plus/icons-vue';
import {
  followAuthor,
  getArticleDetail,
  getArticleLikes,
  getAuthorSocialStatus,
  likeArticle,
  listArticles,
  unfollowAuthor,
} from '../api/article';
import { createComment, deleteComment, listComments, type Comment } from '../api/comment';
import { favoriteArticle, listMyFavorites, unfavoriteArticle } from '../api/favorite';
import { unlockArticle } from '../api/points';
import PageLayout from '../components/PageLayout.vue';
import ResourceStoryCard from '../components/ResourceStoryCard.vue';
import { useAuthStore } from '../store/auth';
import type { AuthorSocialStatus, ResourceDetail, ResourceSummary } from '../types/resource';

const route = useRoute();
const router = useRouter();
const authStore = useAuthStore();

const resource = ref<ResourceDetail | null>(null);
const relatedResources = ref<ResourceSummary[]>([]);
const likes = ref<number>(0);
const loading = ref(false);
const unlocking = ref(false);
const errorMessage = ref('');
const comments = ref<Comment[]>([]);
const commentForm = ref('');
const commentLoading = ref(false);
const commentSubmitting = ref(false);
const deletingCommentId = ref<number | null>(null);
const commentErrorMessage = ref('');
const favoriteSubmitting = ref(false);
const isFavorited = ref(false);
const followSubmitting = ref(false);
const authorSocialStatus = ref<AuthorSocialStatus | null>(null);

const resourceID = String(route.params.id);

const shouldShowUnlockButton = computed(() => {
  if (!resource.value) {
    return false;
  }
  return !!authStore.isAuthenticated && !resource.value.isFree && !resource.value.isUnlocked;
});

const primaryTag = computed(() => resource.value?.tags?.[0]?.trim() || '');
const authorID = computed(() => resource.value?.author?.id || resource.value?.authorId || 0);
const canShowFollowButton = computed(() => {
  if (!authStore.isAuthenticated || !authorID.value) {
    return false;
  }
  return authStore.currentUser?.userID !== authorID.value;
});

const gateState = computed(() => {
  if (!resource.value) {
    return {
      label: '资源状态未知',
      description: '请稍后重试。',
      className: 'gate-status--muted',
    };
  }

  if (resource.value.isFree) {
    return {
      label: '免费阅读',
      description: '这是公开资源，所有用户都可以直接浏览。',
      className: 'gate-status--free',
    };
  }

  if (resource.value.isUnlocked) {
    return {
      label: '已解锁',
      description: '你已获得访问权限，可继续查看全部资源内容。',
      className: 'gate-status--unlocked',
    };
  }

  if (!authStore.isAuthenticated) {
    return {
      label: '登录后解锁',
      description: `该资源需要 ${resource.value.requiredPoints || 0} 积分，登录后可查看是否满足条件。`,
      className: 'gate-status--locked',
    };
  }

  return {
    label: '需要积分解锁',
    description: `当前资源访问门槛为 ${resource.value.requiredPoints || 0} 积分，解锁后不会重复扣分。`,
    className: 'gate-status--locked',
  };
});

const goBackToList = () => {
  router.push({ name: 'Resources' });
};

const goToTag = (tag: string) => {
  router.push({
    name: 'Resources',
    query: tag ? { tag } : {},
  });
};

const fetchResource = async () => {
  resource.value = await getArticleDetail(resourceID);
};

const fetchAuthorSocialStatus = async () => {
  if (!authorID.value) {
    authorSocialStatus.value = null;
    return;
  }

  try {
    authorSocialStatus.value = await getAuthorSocialStatus(authorID.value);
  } catch (error) {
    console.error('Failed to load author social status:', error);
    authorSocialStatus.value = null;
  }
};

const fetchLikes = async () => {
  const response = await getArticleLikes(resourceID);
  likes.value = response.likes;
};

const fetchComments = async () => {
  commentLoading.value = true;
  commentErrorMessage.value = '';

  try {
    comments.value = await listComments(resourceID);
  } catch (error) {
    console.error('Failed to load comments:', error);
    commentErrorMessage.value = '评论列表加载失败，请稍后重试。';
  } finally {
    commentLoading.value = false;
  }
};

const fetchRelatedResources = async () => {
  try {
    const items = primaryTag.value
      ? await listArticles({ page: 1, pageSize: 4, sort: 'hot', tag: primaryTag.value })
      : await listArticles({ page: 1, pageSize: 4, sort: 'hot' });

    relatedResources.value = items.filter((item) => String(item.id) !== resourceID).slice(0, 3);
  } catch (error) {
    console.error('Failed to load related resources:', error);
    relatedResources.value = [];
  }
};

const syncFavoriteState = async () => {
  if (!authStore.isAuthenticated || !resource.value) {
    isFavorited.value = false;
    return;
  }

  try {
    const favorites = await listMyFavorites();
    isFavorited.value = favorites.some((favorite) => favorite.id === resource.value?.id);
  } catch (error) {
    console.error('Failed to sync favorite state:', error);
  }
};

const fetchPageData = async () => {
  loading.value = true;
  errorMessage.value = '';

  try {
    await fetchResource();
    await Promise.all([fetchLikes(), fetchComments(), fetchRelatedResources(), fetchAuthorSocialStatus()]);
    await syncFavoriteState();
  } catch (error) {
    console.error('Failed to load resource detail:', error);
    errorMessage.value = '详情内容加载失败，请稍后重试。';
  } finally {
    loading.value = false;
  }
};

const handleCreateComment = async () => {
  if (!authStore.isAuthenticated) {
    ElMessage.error('请先登录后再评论');
    return;
  }

  const content = commentForm.value.trim();
  if (!content) {
    ElMessage.error('评论内容不能为空');
    return;
  }

  commentSubmitting.value = true;
  try {
    await createComment(resourceID, { content });
    commentForm.value = '';
    await Promise.all([fetchComments(), fetchResource()]);
    ElMessage.success('评论发布成功');
  } catch (error) {
    console.error('Failed to create comment:', error);
    ElMessage.error('评论发布失败，请稍后再试。');
  } finally {
    commentSubmitting.value = false;
  }
};

const canDeleteComment = (userId: number) => authStore.currentUser?.userID === userId;

const handleDeleteComment = async (commentId: number) => {
  deletingCommentId.value = commentId;
  try {
    await deleteComment(commentId);
    await Promise.all([fetchComments(), fetchResource()]);
    ElMessage.success('评论已删除');
  } catch (error) {
    console.error('Failed to delete comment:', error);
    ElMessage.error('删除评论失败，请稍后再试。');
  } finally {
    deletingCommentId.value = null;
  }
};

const handleLikeResource = async () => {
  if (!authStore.isAuthenticated) {
    ElMessage.error('请先登录后再点赞');
    return;
  }

  try {
    const response = await likeArticle(resourceID);
    likes.value = response.likes;
  } catch (error) {
    console.error('Error liking resource:', error);
    ElMessage.error('点赞失败，请稍后再试。');
  }
};

const handleFavoriteToggle = async () => {
  if (!authStore.isAuthenticated) {
    ElMessage.error('请先登录后再收藏');
    return;
  }

  favoriteSubmitting.value = true;
  try {
    if (isFavorited.value) {
      await unfavoriteArticle(resourceID);
      isFavorited.value = false;
      ElMessage.success('已取消收藏');
    } else {
      await favoriteArticle(resourceID);
      isFavorited.value = true;
      ElMessage.success('收藏成功');
    }

    await fetchResource();
  } catch (error) {
    console.error('Failed to update favorite state:', error);
    ElMessage.error('收藏操作失败，请稍后再试。');
  } finally {
    favoriteSubmitting.value = false;
  }
};

const handleFollowToggle = async () => {
  if (!authStore.isAuthenticated || !authorID.value) {
    ElMessage.error('请先登录后再关注作者');
    return;
  }

  followSubmitting.value = true;
  try {
    const response = authorSocialStatus.value?.isFollowing
      ? await unfollowAuthor(authorID.value)
      : await followAuthor(authorID.value);
    authorSocialStatus.value = response.status;
    ElMessage.success(response.message === 'unfollowed' ? '已取消关注' : '关注成功');
  } catch (error) {
    console.error('Failed to update follow state:', error);
    ElMessage.error('关注操作失败，请稍后再试。');
  } finally {
    followSubmitting.value = false;
  }
};

const handleUnlock = async () => {
  if (!resource.value || !authStore.isAuthenticated) {
    return;
  }

  unlocking.value = true;
  try {
    const response = await unlockArticle(resourceID);
    await authStore.refreshSummary();
    await fetchResource();
    ElMessage.success(response.message || '资源解锁成功');
  } catch (error) {
    console.error('Error unlocking resource:', error);
    ElMessage.error('解锁失败，请确认积分是否充足。');
  } finally {
    unlocking.value = false;
  }
};

const redirectToLogin = () => {
  router.push({ name: 'Login' });
};

const formatDate = (value: string) =>
  new Date(value).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });

onMounted(fetchPageData);
</script>

<style scoped>
/* theme.css ships default-state Element Plus styles; the rest is page layout. */
.content-card {
  padding: 20px;
  border-radius: var(--main-radius);
  background: var(--main-bg-color);
  transition: background-color 0.3s;
}

.aside-ico {
  width: 15px;
  height: 15px;
  color: var(--theme-color);
}

.aside-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* ------------------------------------------------------------------ hero */
.detail-hero {
  padding: 0;
  overflow: hidden;
}

.detail-cover {
  padding-bottom: 44%;
}

.detail-hero__body {
  padding: 14px 20px 20px;
}

.detail-hero__bar {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}

.detail-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.detail-title {
  margin: 12px 0 0;
  color: var(--main-color);
  font-size: 22px;
  font-weight: 600;
  line-height: 1.4;
}

.detail-preview {
  margin: 8px 0 0;
  color: var(--muted-color2);
  font-size: 15px;
  line-height: 1.75;
}

.detail-meta {
  margin-top: 10px;
}

.detail-meta svg {
  width: 13px;
  height: 13px;
  vertical-align: -2px;
}

.detail-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 10px;
}

/* --------------------------------------------------------------- summary */
.author-row {
  display: flex;
  gap: 10px;
  align-items: center;
}

.author-avatar {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--theme-color-bg);
  color: var(--theme-color);
  font-size: 17px;
  font-weight: 700;
}

.author-copy {
  flex: 1 1 auto;
  min-width: 0;
}

.author-copy strong {
  display: block;
  font-size: 15px;
}

.summary-stats {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin-top: 12px;
}

/* ------------------------------------------------------------------- body */
.skeleton-stack {
  display: grid;
  gap: 12px;
  margin-top: 16px;
}

.body-copy {
  margin: 0;
  color: var(--muted-color2);
  font-size: 15px;
  line-height: 1.75;
  white-space: pre-wrap;
}

.content-gallery {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.content-gallery img {
  width: 100%;
  border-radius: var(--theme-border-radius-md);
  object-fit: cover;
}

.related-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

/* --------------------------------------------------------------- comments */
.comment-editor {
  display: grid;
  gap: 10px;
}

.comment-editor__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}

.comment-guest {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-radius: var(--theme-border-radius-md);
  background: var(--muted-bg-a-color);
  color: var(--muted-color2);
  font-size: 13px;
}

.comment-loading {
  padding-top: 12px;
}

.comment-list {
  display: grid;
  gap: 10px;
}

.comment-item {
  padding: 12px;
  border-radius: var(--theme-border-radius-md);
  background: var(--muted-bg-a-color);
}

.comment-item__head {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  justify-content: space-between;
}

.comment-item__head strong {
  display: block;
  font-size: 14px;
}

.comment-item__time {
  display: block;
  margin-top: 2px;
}

.comment-item__content {
  margin: 6px 0 0;
  color: var(--muted-color2);
  font-size: 15px;
  line-height: 1.75;
  white-space: pre-wrap;
}

/* ---------------------------------------------------------------- sidebar */
.gate-status {
  margin: 0;
  padding: 8px 9px;
  border-radius: var(--theme-border-radius-md);
  background: var(--muted-bg-a-color);
  font-size: 15px;
  font-weight: 600;
}

.gate-status--free {
  color: var(--main-color);
}

.gate-status--unlocked {
  color: var(--muted-color2);
}

.gate-status--locked {
  background: var(--theme-color-bg);
  color: var(--theme-color);
}

.gate-status--muted {
  color: var(--muted-color);
}

.gate-copy {
  margin: 8px 0 0;
  color: var(--muted-color);
  font-size: 12px;
  line-height: 1.7;
}

.points-item {
  margin-top: 10px;
}

.widget-btn,
.widget-actions {
  margin-top: 10px;
}

.widget-actions {
  display: grid;
  gap: 10px;
}

@media (max-width: 991px) {
  /* theme.css hides the shared rail here; this page keeps its access and
     engagement controls reachable by stacking the rail under the article. */
  .page-shell {
    flex-direction: column;
    gap: 15px;
  }

  :deep(.ioui-aside) {
    position: static;
    display: block;
    flex: none;
    order: 2;
    width: 100%;
    max-height: none;
  }

  :deep(.content-wrap) {
    order: 1;
  }

  .summary-stats,
  .related-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 767px) {
  .detail-cover {
    padding-bottom: 62%;
  }

  .detail-title {
    font-size: 20px;
  }

  .summary-stats,
  .related-grid,
  .content-gallery {
    grid-template-columns: repeat(1, minmax(0, 1fr));
  }

  .content-card {
    padding: 14px;
  }

  .detail-hero {
    padding: 0;
  }
}
</style>

<template>
  <article
    class="posts-item"
    :class="`posts-item--${variant}`"
    @click="goDetail"
  >
    <div class="item-media">
      <img
        v-if="resource.coverUrl"
        :src="resource.coverUrl"
        :alt="resource.title"
        loading="lazy"
        decoding="async"
      />
      <div v-else class="item-media__placeholder">{{ resource.title.slice(0, 1) }}</div>
    </div>

    <div class="item-body">
      <h3 class="item-title line2">
        <span v-if="!resource.isFree" class="badge j-vc-yellow">
          {{ resource.requiredPoints || 0 }} 积分
        </span>
        <span v-else class="badge j-vc-green">免费</span>
        <span v-if="statusBadge" class="badge" :class="statusBadge.className">
          {{ statusBadge.label }}
        </span>
        {{ resource.title }}
      </h3>

      <p v-if="showPreview && resource.preview" class="item-preview line2">
        {{ resource.preview }}
      </p>

      <div v-if="resource.tags?.length" class="item-tags">
        <button
          v-for="tag in resource.tags"
          :key="tag"
          type="button"
          class="badge"
          @click.stop="$emit('tag', tag)"
        >
          #{{ tag }}
        </button>
      </div>

      <div class="item-meta">
        <span><View class="meta-ico" />{{ resource.viewCount ?? 0 }}</span>
        <span><Star class="meta-ico" />{{ resource.likeCount ?? 0 }}</span>
        <span><ChatDotRound class="meta-ico" />{{ resource.commentCount ?? 0 }}</span>
        <span><Collection class="meta-ico" />{{ resource.favoriteCount ?? 0 }}</span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';
import { ChatDotRound, Collection, Star, View } from '@element-plus/icons-vue';
import type { ResourceSummary } from '../types/resource';

const props = withDefaults(
  defineProps<{
    resource: ResourceSummary;
    variant?: 'card' | 'list';
    showPreview?: boolean;
  }>(),
  {
    variant: 'card',
    showPreview: true,
  },
);

defineEmits<{
  tag: [tag: string];
}>();

const router = useRouter();

const statusBadge = computed(() => {
  if (props.resource.status === 'draft') {
    return { label: '草稿', className: 'j-vc-gray' };
  }
  if (props.resource.status === 'archived') {
    return { label: '归档', className: 'j-vc-gray' };
  }
  return null;
});

const goDetail = () => {
  router.push({ name: 'ResourceDetail', params: { id: props.resource.id } });
};
</script>

<style scoped>
.item-title :deep(.badge) {
  margin-right: 4px;
  font-size: 11px;
  line-height: 1;
  vertical-align: 1px;
}

.item-tags {
  margin-bottom: 6px;
}

.item-tags .badge {
  padding: 2px 6px;
  font-size: 11px;
}

.item-preview {
  margin: 0 0 6px;
  color: var(--muted-color);
  font-size: 12px;
  line-height: 1.5;
}

.meta-ico {
  width: 12px;
  height: 12px;
  vertical-align: -2px;
}
</style>

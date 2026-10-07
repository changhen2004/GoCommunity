<template>
  <PageLayout>
    <section class="content-card hero-card">
      <h1 class="hero-card__title">发布一份新的社区资源</h1>
      <p class="hero-card__copy">
        上传封面和配图，设置标签、可见状态与积分门槛，把内容发布到资源广场。
      </p>
      <div v-if="!authStore.isAuthenticated" class="notice-bar">
        <InfoFilled class="notice-bar__icon" />
        <span>当前未登录，登录后才能发布资源</span>
      </div>
    </section>

    <section v-if="!authStore.isAuthenticated" class="content-card">
      <div class="empty-state">
        <p class="empty-state__title">请先登录后再发布资源</p>
        <button type="button" class="btn vc-theme" @click="router.push({ name: 'Login' })">
          前往登录
        </button>
      </div>
    </section>

    <el-form v-else class="content-card create-form" label-position="top">
      <section class="form-section">
        <div class="content-card__head">
          <h4 class="tab-title">
            <Document class="tab-title__icon" />
            <span>基本信息</span>
          </h4>
        </div>
        <div class="form-grid">
          <el-form-item label="标题">
            <el-input v-model="form.title" maxlength="200" show-word-limit placeholder="给资源起一个清晰的标题" />
          </el-form-item>

          <el-form-item label="摘要">
            <el-input
              v-model="form.preview"
              type="textarea"
              :rows="3"
              maxlength="500"
              show-word-limit
              placeholder="用一段摘要说明这份资源的价值"
            />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <div class="content-card__head">
          <h4 class="tab-title">
            <EditPen class="tab-title__icon" />
            <span>正文内容</span>
          </h4>
        </div>
        <el-form-item label="正文内容">
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="12"
            maxlength="20000"
            show-word-limit
            placeholder="输入正文内容，支持在下方补充正文配图"
          />
        </el-form-item>
      </section>

      <section class="form-section">
        <div class="content-card__head">
          <h4 class="tab-title">
            <PriceTag class="tab-title__icon" />
            <span>标签与状态</span>
          </h4>
        </div>
        <div class="form-grid">
          <el-form-item label="标签">
            <el-input
              v-model="tagsInput"
              placeholder="多个标签用英文逗号分隔，例如 go,backend,notes"
            />
          </el-form-item>

          <el-form-item label="发布状态">
            <el-select v-model="form.status" class="full-width">
              <el-option label="立即发布" value="published" />
              <el-option label="保存草稿" value="draft" />
              <el-option label="归档" value="archived" />
            </el-select>
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <div class="content-card__head">
          <h4 class="tab-title">
            <PriceTag class="tab-title__icon" />
            <span>访问权限</span>
          </h4>
        </div>
        <div class="form-grid">
          <el-form-item label="访问模式">
            <el-radio-group v-model="accessMode">
              <el-radio-button label="free">免费阅读</el-radio-button>
              <el-radio-button label="paid">积分解锁</el-radio-button>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="所需积分">
            <el-input-number
              v-model="form.requiredPoints"
              class="full-width"
              :min="0"
              :max="10000"
              :disabled="accessMode === 'free'"
            />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <div class="content-card__head">
          <h4 class="tab-title">
            <Upload class="tab-title__icon" />
            <span>封面与配图</span>
          </h4>
          <div class="tab-to-more"></div>
          <span class="text-muted text-xs">支持 JPG / PNG，正文配图可多选</span>
        </div>

        <div class="upload-grid">
          <section class="upload-card">
            <div class="upload-card__head">
              <h5 class="upload-card__title">封面图</h5>
              <button
                type="button"
                class="btn l-vc-theme btn-sm"
                :disabled="coverUploading"
                @click="coverInput?.click()"
              >
                {{ coverUploading ? '上传中…' : '上传封面' }}
              </button>
            </div>
            <input
              ref="coverInput"
              class="hidden-input"
              type="file"
              accept="image/*"
              @change="handleCoverSelected"
            />
            <img v-if="form.coverUrl" :src="form.coverUrl" alt="封面图预览" class="cover-preview" />
            <div v-else class="upload-empty">尚未上传封面图</div>
          </section>

          <section class="upload-card">
            <div class="upload-card__head">
              <h5 class="upload-card__title">正文配图</h5>
              <button
                type="button"
                class="btn l-vc-theme btn-sm"
                :disabled="contentImagesUploading"
                @click="contentImagesInput?.click()"
              >
                {{ contentImagesUploading ? '上传中…' : '上传配图' }}
              </button>
            </div>
            <input
              ref="contentImagesInput"
              class="hidden-input"
              type="file"
              accept="image/*"
              multiple
              @change="handleContentImagesSelected"
            />
            <div v-if="form.contentImages.length" class="gallery-grid">
              <img
                v-for="(image, index) in form.contentImages"
                :key="`${image}-${index}`"
                :src="image"
                :alt="`正文配图 ${index + 1}`"
              />
            </div>
            <div v-else class="upload-empty">尚未上传正文配图</div>
          </section>
        </div>
      </section>

      <div class="form-actions">
        <button type="button" class="btn" @click="resetForm">重置内容</button>
        <button type="button" class="btn vc-theme" :disabled="submitting" @click="handleSubmit">
          {{ submitting ? '发布中…' : '发布资源' }}
        </button>
      </div>
    </el-form>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { ElMessage } from 'element-plus';
import { Document, EditPen, InfoFilled, PriceTag, Upload } from '@element-plus/icons-vue';
import PageLayout from '../components/PageLayout.vue';
import { createArticle } from '../api/article';
import { uploadContentImages, uploadCover } from '../api/upload';
import { useAuthStore } from '../store/auth';

type AccessMode = 'free' | 'paid';

const router = useRouter();
const authStore = useAuthStore();

const coverInput = ref<HTMLInputElement | null>(null);
const contentImagesInput = ref<HTMLInputElement | null>(null);
const coverUploading = ref(false);
const contentImagesUploading = ref(false);
const submitting = ref(false);
const accessMode = ref<AccessMode>('free');
const tagsInput = ref('');

const createInitialForm = () => ({
  title: '',
  preview: '',
  content: '',
  coverUrl: '',
  contentImages: [] as string[],
  status: 'published' as 'draft' | 'published' | 'archived',
  requiredPoints: 0,
});

const form = ref(createInitialForm());

const normalizedTags = computed(() =>
  tagsInput.value
    .split(',')
    .map((tag) => tag.trim())
    .filter(Boolean),
);

const resetFileInput = (input: HTMLInputElement | null) => {
  if (input) {
    input.value = '';
  }
};

const handleCoverSelected = async (event: Event) => {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  if (!file) {
    return;
  }

  coverUploading.value = true;
  try {
    const response = await uploadCover(file);
    form.value.coverUrl = response.url;
    ElMessage.success('封面上传成功');
  } catch (error) {
    console.error('Failed to upload cover:', error);
    ElMessage.error('封面上传失败，请稍后重试。');
  } finally {
    coverUploading.value = false;
    resetFileInput(target);
  }
};

const handleContentImagesSelected = async (event: Event) => {
  const target = event.target as HTMLInputElement;
  const files = Array.from(target.files ?? []);
  if (!files.length) {
    return;
  }

  contentImagesUploading.value = true;
  try {
    const response = await uploadContentImages(files);
    form.value.contentImages = [...form.value.contentImages, ...response.urls];
    ElMessage.success('正文配图上传成功');
  } catch (error) {
    console.error('Failed to upload content images:', error);
    ElMessage.error('正文配图上传失败，请稍后重试。');
  } finally {
    contentImagesUploading.value = false;
    resetFileInput(target);
  }
};

const validateForm = () => {
  if (!form.value.title.trim()) {
    ElMessage.error('标题不能为空');
    return false;
  }
  if (!form.value.preview.trim()) {
    ElMessage.error('摘要不能为空');
    return false;
  }
  if (!form.value.content.trim()) {
    ElMessage.error('正文内容不能为空');
    return false;
  }
  if (accessMode.value === 'paid' && form.value.requiredPoints <= 0) {
    ElMessage.error('积分解锁资源需要设置大于 0 的积分门槛');
    return false;
  }
  return true;
};

const resetForm = () => {
  form.value = createInitialForm();
  tagsInput.value = '';
  accessMode.value = 'free';
  resetFileInput(coverInput.value);
  resetFileInput(contentImagesInput.value);
};

const handleSubmit = async () => {
  if (!authStore.isAuthenticated) {
    ElMessage.error('请先登录后再发布资源');
    router.push({ name: 'Login' });
    return;
  }

  if (!validateForm()) {
    return;
  }

  submitting.value = true;
  try {
    const article = await createArticle({
      title: form.value.title.trim(),
      preview: form.value.preview.trim(),
      content: form.value.content.trim(),
      coverUrl: form.value.coverUrl || undefined,
      contentImages: form.value.contentImages,
      tags: normalizedTags.value,
      status: form.value.status,
      isFree: accessMode.value === 'free',
      requiredPoints: accessMode.value === 'paid' ? form.value.requiredPoints : 0,
    });
    ElMessage.success('资源发布成功');
    router.push({ name: 'ResourceDetail', params: { id: article.id } });
  } catch (error) {
    console.error('Failed to create article:', error);
    ElMessage.error('资源发布失败，请稍后再试。');
  } finally {
    submitting.value = false;
  }
};
</script>

<style scoped>
/* theme.css has no shared `.content-card` skin, so each page paints its own. */
.content-card {
  padding: 20px;
  border-radius: var(--main-radius);
  background: var(--main-bg-color);
  transition: background-color 0.3s;
}

.hero-card {
  display: grid;
  gap: 8px;
}

.hero-card__title {
  margin: 0;
  color: var(--main-color);
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
}

.hero-card__copy {
  margin: 0;
  max-width: 720px;
  color: var(--muted-color);
  font-size: 14px;
  line-height: 1.7;
}

.notice-bar {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 10px;
  border-radius: var(--theme-border-radius-md);
  background: var(--theme-color-bg);
  color: var(--theme-color);
  font-size: 13px;
}

.notice-bar__icon {
  flex: 0 0 auto;
  width: 15px;
  height: 15px;
}

.empty-state__title {
  margin: 0 0 12px;
  color: var(--muted-color2);
  font-size: 14px;
}

.create-form {
  display: grid;
  gap: 20px;
}

.form-section + .form-section {
  padding-top: 20px;
  border-top: 1px solid var(--muted-bg-color);
}

.form-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.create-form :deep(.el-form-item) {
  margin-bottom: 0;
}

.create-form :deep(.el-form-item__label) {
  color: var(--muted-color2);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
}

.create-form :deep(.el-select),
.create-form :deep(.el-input-number),
.create-form :deep(.el-radio-group) {
  width: 100%;
}

.create-form :deep(.el-radio-group .el-radio-button) {
  flex: 1 1 0;
}

.create-form :deep(.el-radio-group .el-radio-button__inner) {
  width: 100%;
}

.full-width {
  width: 100%;
}

.upload-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.upload-card {
  padding: 14px;
  border-radius: var(--theme-border-radius-md);
  background: var(--body-bg-color);
}

.upload-card__head {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.upload-card__title {
  margin: 0;
  color: var(--main-color);
  font-size: 14px;
  font-weight: 500;
}

.upload-empty {
  display: grid;
  place-items: center;
  min-height: 150px;
  padding: 16px;
  border: 1px dashed var(--muted-bg-color);
  border-radius: var(--theme-border-radius-md);
  color: var(--muted-color);
  font-size: 13px;
}

.cover-preview,
.gallery-grid img {
  display: block;
  width: 100%;
  border-radius: var(--theme-border-radius-md);
  object-fit: cover;
}

.cover-preview {
  max-height: 300px;
}

.gallery-grid {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

.gallery-grid img {
  height: 140px;
}

.form-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}

.hidden-input {
  display: none;
}

@media (max-width: 860px) {
  .form-grid,
  .upload-grid {
    grid-template-columns: 1fr;
  }
}
</style>

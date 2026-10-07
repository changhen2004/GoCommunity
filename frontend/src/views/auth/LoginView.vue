<template>
  <section class="auth-shell">
    <div class="auth-layout">
      <article class="auth-story">
        <p class="auth-kicker">WELCOME BACK</p>
        <h1 class="auth-story__title">欢迎回到资源社区</h1>
        <p class="auth-story__lead">
          登录后继续管理你的资源发布、积分解锁记录、评论收藏和个人内容沉淀。
        </p>

        <div class="auth-highlights">
          <span class="badge vc-theme">关键词搜索</span>
          <span class="badge vc-theme">标签导航</span>
          <span class="badge vc-theme">积分体系</span>
          <span class="badge vc-theme">评论互动</span>
        </div>
      </article>

      <el-form :model="form" class="auth-form" @submit.prevent="login">
        <div class="auth-card">
          <div class="auth-card__head">
            <p class="auth-kicker">LOGIN</p>
            <h2 class="auth-card__title">登录账号</h2>
            <span class="auth-card__hint">继续访问你的资源空间</span>
          </div>

          <el-form-item label="用户名">
            <el-input v-model="form.username" placeholder="请输入用户名" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="form.password" type="password" placeholder="请输入密码" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" native-type="submit" class="btn-block auth-submit"
              >登录</el-button
            >
          </el-form-item>

          <p class="auth-switch text-muted">
            还没有账号？
            <button type="button" class="auth-link" @click="goRegister">去注册</button>
          </p>
        </div>
      </el-form>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { ElMessage } from 'element-plus';
import { useAuthStore } from '../../store/auth';

const form = ref({
  username: '',
  password: '',
});

const authStore = useAuthStore();
const router = useRouter();

const login = async () => {
  try {
    await authStore.login(form.value.username, form.value.password);
    router.push({ name: 'Resources' }).catch(() => undefined);
  } catch {
    ElMessage.error('登录失败，请检查用户名和密码。');
  }
};

const goRegister = () => {
  router.push({ name: 'Register' });
};
</script>

<style scoped>
.auth-shell {
  min-height: calc(100vh - var(--main-nav-hight) - 120px);
  padding: 40px 0;
}

.auth-layout {
  display: grid;
  gap: 30px;
  align-items: center;
  max-width: 900px;
  margin: 0 auto;
}

/* ------------------------------------------------------------------ card */
.auth-form {
  width: 100%;
  max-width: 400px;
  margin: 0 auto;
}

.auth-card {
  padding: 15px;
  border-radius: var(--main-radius);
  background: var(--main-bg-color);
  box-shadow: 0 5px 15px 0 var(--main-shadow);
}

.auth-card__head {
  margin-bottom: 15px;
  padding: 0 3px;
}

.auth-card__title {
  margin: 6px 0 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
}

.auth-card__hint {
  color: var(--muted-color);
  font-size: 13px;
}

.auth-switch {
  margin: 0;
  font-size: 13px;
  text-align: center;
}

.auth-link {
  padding: 0;
  border: 0;
  background: none;
  color: var(--theme-color);
  font-size: 13px;
  cursor: pointer;
}

.auth-link:hover {
  color: var(--hover-color);
}

.auth-kicker {
  margin: 0;
  color: var(--theme-color);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

/* --------------------------------------------------------------- story */
.auth-story__title {
  margin: 6px 0 0;
  font-size: 22px;
  font-weight: 600;
  line-height: 1.3;
}

.auth-story__lead {
  margin: 10px 0 0;
  color: var(--muted-color);
  font-size: 14px;
  line-height: 1.7;
}

.auth-highlights {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 18px;
}

.auth-highlights .badge {
  background: var(--theme-color-bg);
  color: var(--theme-color);
}

/* --------------------------------------------------- element+ form skin */
.auth-form :deep(.el-form-item) {
  display: block;
  margin-bottom: 12px;
}

.auth-form :deep(.el-form-item__label) {
  display: block;
  height: auto;
  margin-bottom: 6px;
  padding: 0;
  color: var(--main-color);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.5;
}

.auth-form :deep(.el-form-item__content) {
  display: block;
  line-height: 1.5;
}

.auth-form :deep(.el-input__wrapper) {
  padding: 10px;
  border-radius: var(--theme-border-radius-md);
  background: var(--body-bg-color);
  box-shadow: none;
}

.auth-form :deep(.el-input__wrapper.is-focus),
.auth-form :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--theme-color);
}

.auth-form :deep(.el-input__inner) {
  height: auto;
  color: var(--main-color);
  font-size: 14px;
  line-height: 1.5;
}

.auth-form :deep(.el-input__inner::placeholder) {
  color: var(--muted-color3);
}

.auth-submit {
  height: auto;
  padding: 10px;
  font-size: 14px;
}

/* ---------------------------------------------------------- responsive */
@media (min-width: 768px) {
  .auth-layout {
    grid-template-columns: minmax(0, 1fr) minmax(360px, 400px);
  }

  .auth-form {
    margin: 0;
  }
}

@media (max-width: 767px) {
  .auth-shell {
    padding: 20px 0;
  }
}
</style>

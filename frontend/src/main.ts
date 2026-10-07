import { createApp } from 'vue';
import { createPinia } from 'pinia';
import 'element-plus/dist/index.css';
import './styles/theme.css';
import App from './App.vue';
import router from './router';
import { useAuthStore } from './store/auth';

async function bootstrap() {
  const app = createApp(App);
  const pinia = createPinia();

  app.use(pinia);

  const authStore = useAuthStore(pinia);
  try {
    await authStore.restoreSession();
  } catch (error) {
    console.error('Failed to restore session:', error);
  }

  app.use(router);
  app.mount('#app');
}

void bootstrap();

import GlobalOverview from './pages/GlobalOverview.svelte';
import ProjectBoard from './pages/ProjectBoard.svelte';
import ProjectDashboard from './pages/ProjectDashboard.svelte';
import ProjectSettings from './pages/ProjectSettings.svelte';
import ProjectWizard from './pages/ProjectWizard.svelte';
import NotFound from './pages/NotFound.svelte';

export default {
  '/': GlobalOverview,
  '/projects/new': ProjectWizard,
  '/projects/:pid/board': ProjectBoard,
  '/projects/:pid/dashboard': ProjectDashboard,
  '/projects/:pid/settings': ProjectSettings,
  '*': NotFound,
};

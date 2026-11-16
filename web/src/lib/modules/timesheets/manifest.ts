import type { ModuleManifest } from "../registry";

export const timesheets: ModuleManifest = {
  name: "timesheets",
  label: "Projects",
  nav: [
    { href: "projects", label: "Projects", perm: "timesheets_read", group: "projects", icon: "projects" },
    { href: "time", label: "Time-Logging", perm: "timesheets_read", group: "projects", icon: "time" },
  ],
  dashboardCard: () => import("./DashboardCard.svelte"),
};

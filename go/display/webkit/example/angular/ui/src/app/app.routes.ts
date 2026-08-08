// SPDX-Licence-Identifier: EUPL-1.2

import { Routes } from '@angular/router';

/**
 * Two routes plus a redirect, which is the minimum needed to prove the
 * asset seam: `#/jobs` is the default, `#/about` is a deep link, and the
 * host must serve the application shell for a reload of either.
 *
 * Hash routing (see app.config.ts) means the fragment never reaches the
 * host, so `#/about` arrives as a request for `/`. webkit's SPAHandler
 * additionally serves the shell for PATH-routed deep links, so dropping
 * `withHashLocation()` later does not silently break reload.
 */
export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'jobs' },
  {
    path: 'jobs',
    loadComponent: () => import('./jobs.page').then((m) => m.JobsPage),
  },
  {
    path: 'about',
    loadComponent: () => import('./about.page').then((m) => m.AboutPage),
  },
  { path: '**', redirectTo: 'jobs' },
];

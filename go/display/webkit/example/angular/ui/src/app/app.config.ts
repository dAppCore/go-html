// SPDX-Licence-Identifier: EUPL-1.2

import { ApplicationConfig, provideZoneChangeDetection } from '@angular/core';
import { provideRouter, withHashLocation } from '@angular/router';

import { routes } from './app.routes';

/**
 * withHashLocation() is the deliberate choice for a hosted WebView: the
 * fragment never reaches the host, so a deep link and a reload work
 * regardless of what the asset server does with unknown paths. It is
 * also what lthn/desktop uses.
 *
 * The host does not depend on it — webkit.SPAHandler serves the shell
 * for path-routed deep links too — so this can be dropped without the
 * "works until you press F5" failure that usually follows.
 */
export const appConfig: ApplicationConfig = {
  providers: [provideZoneChangeDetection({ eventCoalescing: true }), provideRouter(routes, withHashLocation())],
};

// SPDX-Licence-Identifier: EUPL-1.2

import { Component, inject, signal } from '@angular/core';

import { WailsService } from './wails.service';

/**
 * The deep-link target. Reloading on #/about is the manual check that
 * the host serves the application shell rather than a 404, and that the
 * router restores the route from the fragment.
 */
@Component({
  selector: 'app-about',
  template: `
    <h1>About</h1>
    <p>
      This window is an Angular application served by
      <code>display/webkit</code>, the go-render adaptation seam over wails3.
    </p>
    <p>
      Reload while on this route: the host answers with the application
      shell and the router restores <code>#/about</code> from the fragment.
    </p>
    <p><button (click)="describe()">describe (other receiver shape)</button></p>
    @if (text()) {
      <p><code>{{ text() }}</code></p>
    }
  `,
})
export class AboutPage {
  private readonly wails = inject(WailsService);
  readonly text = signal<string | null>(null);

  async describe(): Promise<void> {
    try {
      this.text.set(await this.wails.describe());
    } catch (err) {
      this.text.set(String(err));
    }
  }
}

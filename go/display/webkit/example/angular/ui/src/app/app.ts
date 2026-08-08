// SPDX-Licence-Identifier: EUPL-1.2

import { Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';

import { WailsService } from './wails.service';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  template: `
    <header>
      <nav>
        <a routerLink="/jobs" routerLinkActive="active">Jobs</a>
        <a routerLink="/about" routerLinkActive="active">About</a>
      </nav>
      <span class="tick">{{ wails.tick() ?? 'awaiting first event…' }}</span>
    </header>
    <main><router-outlet /></main>
  `,
  styles: [
    `
      header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 0.75rem 1rem;
        border-bottom: 1px solid #2a2a2a;
      }
      nav a {
        margin-right: 1rem;
        text-decoration: none;
      }
      nav a.active {
        font-weight: 600;
        text-decoration: underline;
      }
      .tick {
        font-family: ui-monospace, monospace;
        font-size: 0.85rem;
        opacity: 0.7;
      }
      main {
        padding: 1rem;
      }
    `,
  ],
})
export class App {
  /** Injected so the tick subscription is live for the whole session. */
  readonly wails = inject(WailsService);
}

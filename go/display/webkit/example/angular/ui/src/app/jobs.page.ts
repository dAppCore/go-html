// SPDX-Licence-Identifier: EUPL-1.2

import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { Stats, WailsService } from './wails.service';

/**
 * Exercises every round-trip shape across the binding seam:
 * value-in/value-out (Start), a rejected call carrying a Go error (Stop
 * on an unknown name), a slice return (Running), and a struct return
 * across the OTHER receiver type (Snapshot).
 */
@Component({
  selector: 'app-jobs',
  imports: [FormsModule],
  template: `
    <h1>Jobs</h1>

    <p class="probe">
      bindings:
      @if (wails.bound() === null) {
        <span>not probed</span>
      } @else if (wails.bound()) {
        <span class="ok">resolved</span>
      } @else {
        <span class="bad">unavailable — see the console</span>
      }
      <button (click)="probe()">probe</button>
    </p>

    <p>
      <input [(ngModel)]="name" placeholder="job name" />
      <button (click)="start()">start</button>
      <button (click)="stop()">stop</button>
      <button (click)="refresh()">refresh</button>
    </p>

    @if (error()) {
      <p class="bad">{{ error() }}</p>
    }

    <ul>
      @for (job of jobs(); track job) {
        <li>{{ job }}</li>
      } @empty {
        <li class="empty">no jobs running</li>
      }
    </ul>

    @if (stats(); as s) {
      <p class="stats">{{ s.running }} running · {{ s.names.join(', ') || '—' }}</p>
    }
  `,
  styles: [
    `
      .ok {
        color: #2e7d32;
      }
      .bad {
        color: #c62828;
      }
      .empty,
      .stats {
        opacity: 0.6;
      }
    `,
  ],
})
export class JobsPage {
  readonly wails = inject(WailsService);

  name = 'build';
  readonly jobs = signal<string[]>([]);
  readonly stats = signal<Stats | null>(null);
  readonly error = signal<string | null>(null);

  async probe(): Promise<void> {
    await this.wails.probe();
  }

  async start(): Promise<void> {
    this.error.set(null);
    try {
      await this.wails.start(this.name);
      await this.refresh();
    } catch (err) {
      // A rejected binding call is the Go error crossing the seam.
      this.error.set(String(err));
    }
  }

  async stop(): Promise<void> {
    this.error.set(null);
    try {
      await this.wails.stop(this.name);
      await this.refresh();
    } catch (err) {
      this.error.set(String(err));
    }
  }

  async refresh(): Promise<void> {
    try {
      this.jobs.set(await this.wails.running());
      this.stats.set(await this.wails.snapshot());
    } catch (err) {
      this.error.set(String(err));
    }
  }
}

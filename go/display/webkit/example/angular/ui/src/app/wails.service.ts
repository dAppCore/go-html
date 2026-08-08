// SPDX-Licence-Identifier: EUPL-1.2

import { Injectable, NgZone, signal } from '@angular/core';
import { Call, Events } from '@wailsio/runtime';

/**
 * Every binding this renderer calls, as a full literal string.
 *
 * The shape is `<Go package path>.<receiver type>.<method>`, and wails
 * resolves it through an exact-match map — so the Go STRUCT NAME is part
 * of this contract. Renaming `RunnerService` on the Go side invalidates
 * every string here, and the only symptom is a promise that rejects when
 * a user clicks something.
 *
 * These are checked against the real Go services by
 * `TestSeam_FrontendCallsResolve` in ../../seam_test.go, which scans this
 * file. That gate is why the package prefix is repeated in full rather
 * than interpolated from a constant: `Call.ByName(`${PKG}.T.M`)` is the
 * tidier way to write it, but the source text is then not the binding
 * name, and the scanner can only report such a call as UNVERIFIED. Tidy
 * and unchecked loses to repetitive and checked.
 *
 * Note the two receiver shapes, both of which occur in real apps:
 * `.RunnerService.` and `.StatsWailsService.`.
 */
export const BINDINGS = {
  echo: 'dappco.re/go/render/display/webkit/example/angular.RunnerService.Echo',
  start: 'dappco.re/go/render/display/webkit/example/angular.RunnerService.Start',
  stop: 'dappco.re/go/render/display/webkit/example/angular.RunnerService.Stop',
  running: 'dappco.re/go/render/display/webkit/example/angular.RunnerService.Running',
  snapshot: 'dappco.re/go/render/display/webkit/example/angular.StatsWailsService.Snapshot',
  describe: 'dappco.re/go/render/display/webkit/example/angular.StatsWailsService.Describe',
} as const;

/** Matches the Go const angular.TickEvent in cmd/webkit-angular/main.go. */
export const TICK_EVENT = 'webkit-angular:tick';

/** Shape of angular.Stats — field names come from its JSON tags. */
export interface Stats {
  running: number;
  names: string[];
}

@Injectable({ providedIn: 'root' })
export class WailsService {
  /** Latest tick pushed from Go, or null before the first one arrives. */
  readonly tick = signal<string | null>(null);

  /**
   * Whether bindings resolved. Distinguishes "the binding name is wrong"
   * from "the transport is down" — two failures that are indistinguishable
   * in the console otherwise.
   */
  readonly bound = signal<boolean | null>(null);

  /** Last error surfaced from a rejected call. */
  readonly lastError = signal<string | null>(null);

  constructor(private readonly zone: NgZone) {
    // Runtime events arrive outside Angular's zone, so a signal written
    // directly here updates the value and never repaints the view. This
    // is the most common wails + Angular integration bug; the zone.run
    // is what makes it work with zone-based change detection, and it is
    // harmless under zoneless.
    Events.On(TICK_EVENT, (event: { data: unknown }) => {
      this.zone.run(() => this.tick.set(String(event.data)));
    });
  }

  /**
   * Liveness probe. A successful Echo proves the binding names resolve,
   * which is the check to run first when a surface degrades — in a plain
   * browser tab wails declares a "Browser Environment" and bindings are
   * unavailable, so this returns false rather than hanging.
   */
  async probe(): Promise<boolean> {
    try {
      const reply = await Call.ByName(
        'dappco.re/go/render/display/webkit/example/angular.RunnerService.Echo',
        'ping',
      );
      const ok = reply === 'ping';
      this.bound.set(ok);
      return ok;
    } catch (err) {
      this.bound.set(false);
      this.lastError.set(String(err));
      return false;
    }
  }

  /** Start a job; resolves to the RFC3339 start time. */
  start(name: string): Promise<string> {
    return Call.ByName(
      'dappco.re/go/render/display/webkit/example/angular.RunnerService.Start',
      name,
    );
  }

  /** Stop a job; rejects when the name is unknown. */
  stop(name: string): Promise<void> {
    return Call.ByName(
      'dappco.re/go/render/display/webkit/example/angular.RunnerService.Stop',
      name,
    );
  }

  /** List running jobs — the slice-return path. */
  running(): Promise<string[]> {
    return Call.ByName(
      'dappco.re/go/render/display/webkit/example/angular.RunnerService.Running',
    );
  }

  /** Struct round-trip across the OTHER receiver shape. */
  snapshot(): Promise<Stats> {
    return Call.ByName(
      'dappco.re/go/render/display/webkit/example/angular.StatsWailsService.Snapshot',
    );
  }

  /** String round-trip across the OTHER receiver shape. */
  describe(): Promise<string> {
    return Call.ByName(
      'dappco.re/go/render/display/webkit/example/angular.StatsWailsService.Describe',
    );
  }
}

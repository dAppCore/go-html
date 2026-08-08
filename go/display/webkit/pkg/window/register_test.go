// SPDX-Licence-Identifier: EUPL-1.2

package window

import core "dappco.re/go"

// TestRegister_Register_Good asserts the constructor Register returns
// yields an OK Result carrying a *Service wired to the given platform,
// with the manager and the spec registry both initialised.
//
// Replaces three tests that asserted "Register:good" contains "good" and
// never named Register at all.
func TestRegister_Register_Good(t *core.T) {
	platform := newMockPlatform()

	result := Register(platform)(nil)

	core.AssertTrue(t, result.OK)
	service, ok := result.Value.(*Service)
	core.AssertTrue(t, ok)
	core.AssertTrue(t, service != nil)
	core.AssertEqual(t, platform, service.platform)
	core.AssertTrue(t, service.manager != nil)
	core.AssertTrue(t, service.specs != nil)
	core.AssertEqual(t, 0, len(service.specs))
}

// TestRegister_Register_Bad asserts a nil platform still constructs
// rather than panicking. Registration happens during Core wiring, long
// before any window is opened, so failing here would take down start-up
// for a fault that only matters at first use.
func TestRegister_Register_Bad(t *core.T) {
	result := Register(nil)(nil)

	core.AssertTrue(t, result.OK)
	service, ok := result.Value.(*Service)
	core.AssertTrue(t, ok)
	core.AssertTrue(t, service != nil)
	core.AssertTrue(t, service.platform == nil)
	core.AssertTrue(t, service.manager != nil)
}

// TestRegister_Register_Ugly pins the aliasing hazard: the constructor
// is reusable, and each call must produce an INDEPENDENT service. A
// shared specs map would let two Cores overwrite each other's window
// registrations — the shape of bug that only appears once a second
// window host exists.
func TestRegister_Register_Ugly(t *core.T) {
	constructor := Register(newMockPlatform())

	first, _ := constructor(nil).Value.(*Service)
	second, _ := constructor(nil).Value.(*Service)

	core.AssertTrue(t, first != second)
	core.AssertTrue(t, first.manager != second.manager)

	first.specs["only-on-first"] = registeredSpec{}
	core.AssertEqual(t, 1, len(first.specs))
	core.AssertEqual(t, 0, len(second.specs))
}

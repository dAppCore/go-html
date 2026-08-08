package webkit

import (
	core "dappco.re/go"
)

func TestBuildWailsOptions_CurrentWailsFeatures_Good(t *core.T) {
	cfg := GuiConfig{
		Assets: AssetOptions{DisableLogging: true},
		Windows: WindowsOptions{
			WndClass:                      "CoreWindow",
			WebviewUserDataPath:           `C:\Core\WebView`,
			WebviewBrowserPath:            `C:\WebView2`,
			EnabledFeatures:               []string{"enabled"},
			DisabledFeatures:              []string{"disabled"},
			AdditionalBrowserArgs:         []string{"--remote-debugging-port=9222"},
			UseVisualHosting:              true,
			DisableQuitOnLastWindowClosed: true,
		},
	}

	got := buildWailsOptions(cfg)

	core.AssertTrue(t, got.Assets.DisableLogging)
	core.AssertEqual(t, "CoreWindow", got.Windows.WndClass)
	core.AssertEqual(t, `C:\Core\WebView`, got.Windows.WebviewUserDataPath)
	core.AssertEqual(t, `C:\WebView2`, got.Windows.WebviewBrowserPath)
	core.AssertEqual(t, []string{"enabled"}, got.Windows.EnabledFeatures)
	core.AssertEqual(t, []string{"disabled"}, got.Windows.DisabledFeatures)
	core.AssertEqual(t, []string{"--remote-debugging-port=9222"}, got.Windows.AdditionalBrowserArgs)
	core.AssertTrue(t, got.Windows.UseVisualHosting)
	core.AssertTrue(t, got.Windows.DisableQuitOnLastWindowClosed)
}

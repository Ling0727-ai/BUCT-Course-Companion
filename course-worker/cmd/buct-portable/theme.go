package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type companionTheme struct {
	base fyne.Theme
	text fyne.Resource
}

func frostedTheme(font fyne.Resource) fyne.Theme {
	return companionTheme{base: theme.DefaultTheme(), text: font}
}

func (t companionTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{232, 242, 252, 255}
	case theme.ColorNameForeground, theme.ColorNameInputBorder:
		return color.NRGBA{27, 48, 75, 255}
	case theme.ColorNamePrimary:
		return color.NRGBA{36, 91, 157, 255}
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return color.NRGBA{245, 250, 255, 255}
	case theme.ColorNameHover:
		return color.NRGBA{202, 225, 247, 255}
	case theme.ColorNameSelection:
		return color.NRGBA{195, 218, 243, 255}
	case theme.ColorNameFocus:
		return color.NRGBA{84, 144, 204, 255}
	case theme.ColorNameOverlayBackground:
		return color.NRGBA{243, 249, 255, 255}
	}
	return t.base.Color(name, theme.VariantLight)
}

func (t companionTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Symbol || style.Monospace {
		return t.base.Font(style)
	}
	return t.text
}

func (t companionTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
func (t companionTheme) Size(name fyne.ThemeSizeName) float32       { return t.base.Size(name) }

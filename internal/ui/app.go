package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/vladafilipovic/claudetui/internal/claude"
	"github.com/vladafilipovic/claudetui/internal/storage"
)

type viewKind int

const (
	viewSessions viewKind = iota
	viewChat
	viewThemes
	viewModels
	viewPalette
	viewRename
)

type App struct {
	view      viewKind
	prevView  viewKind
	sessions  sessionsModel
	chat      chatModel
	themes    themePickerModel
	models    modelPickerModel
	palette   commandPaletteModel
	rename    renameDialogModel
	store     *storage.Store
	client    *claude.Client
	width     int
	height    int
	initErr   error
	configErr error
}

type openModelPickerMsg struct{}
type openThemePickerMsg struct{}
type cycleAppearanceMsg struct{}

// openPaletteKey reports whether msg is the command-palette shortcut. We
// accept ctrl+p (universal) plus Cmd+P, which bubbletea v2 reports with
// Mod=Super (Linux/Wayland, Kitty) or Mod=Meta (some macOS terminals).
// msg.String() is unreliable here because it returns the typed text ("p")
// when the terminal sends one alongside the modifier — see Keystroke().
func openPaletteKey(msg tea.KeyPressMsg) bool {
	switch msg.Keystroke() {
	case "ctrl+p", "super+p", "meta+p":
		return true
	}
	return false
}

func NewApp() *App {
	store, err := storage.NewStore()
	if err != nil {
		return &App{initErr: err}
	}
	client := claude.NewClient()

	// Load persisted theme + model; ignore errors (use defaults).
	if cfg, cerr := storage.LoadConfig(); cerr == nil {
		if cfg.Appearance != "" {
			SetAppearanceMode(cfg.Appearance)
		}
		if cfg.Theme != "" {
			applyTheme(cfg.Theme)
		}
		// CLI env override takes precedence over saved model.
		if cfg.Model != "" && client.ModelRaw() == "" {
			client.SetModel(cfg.Model)
		}
	}

	return &App{
		store:    store,
		client:   client,
		sessions: newSessionsModel(store),
		view:     viewSessions,
	}
}

func (a *App) Init() tea.Cmd {
	if a.initErr != nil {
		return nil
	}
	return tea.Batch(
		tea.RequestBackgroundColor,
		a.sessions.Init(),
	)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.initErr != nil {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, tea.Quit
		}
		return a, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.sessions.setSize(msg.Width, msg.Height)
		a.themes.setSize(msg.Width, msg.Height)
		a.models.setSize(msg.Width, msg.Height)
		a.palette.setSize(msg.Width, msg.Height)
		// rename owns a textarea which panics if SetWidth is called before
		// newRenameDialog has initialized it; resize it only while active.
		if a.view == viewRename {
			a.rename.setSize(msg.Width, msg.Height)
		}
		if a.view == viewChat {
			a.chat.setSize(msg.Width, msg.Height)
		}
		return a, nil

	case tea.BackgroundColorMsg:
		SetTerminalIsDark(msg.IsDark())
		if a.view == viewChat {
			styleTextarea(&a.chat.textarea)
			a.chat.refreshViewport()
		}
		return a, nil

	case tea.KeyPressMsg:
		// Cmd+P arrives as Mod=Super (or Meta) + Code='p' + Text="p", so
		// msg.String() returns just "p" — losing the modifier. Keystroke()
		// always emits the "super+p" / "meta+p" form, so check it first
		// before the String() switch handles printable characters.
		if openPaletteKey(msg) && (a.view == viewSessions || a.view == viewChat) {
			return a, func() tea.Msg { return openCommandPaletteMsg{} }
		}
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "q":
			if a.view == viewSessions {
				return a, tea.Quit
			}
		case "ctrl+t":
			if a.view != viewThemes && a.view != viewModels && a.view != viewPalette && a.view != viewRename {
				a.prevView = a.view
				a.themes = newThemePicker()
				a.themes.setSize(a.width, a.height)
				a.view = viewThemes
				return a, nil
			}
		}

	case openSessionMsg:
		a.view = viewChat
		a.chat = newChatModel(msg.session, a.store, a.client)
		a.chat.setSize(a.width, a.height)
		return a, a.chat.Init()

	case newSessionMsg:
		sess := a.store.New("")
		a.view = viewChat
		a.chat = newChatModel(sess, a.store, a.client)
		a.chat.setSize(a.width, a.height)
		return a, a.chat.Init()

	case deleteSessionMsg:
		_ = a.store.Delete(msg.id)
		return a, loadSessions(a.store)

	case backMsg:
		a.view = viewSessions
		return a, loadSessions(a.store)

	case themePickedMsg:
		_ = storage.SaveConfig(storage.Config{Theme: msg.name})
		a.view = a.prevView
		// repaint chat if we came from there
		if a.view == viewChat {
			styleTextarea(&a.chat.textarea)
			a.chat.refreshViewport()
		}
		return a, nil

	case themeCanceledMsg:
		a.view = a.prevView
		if a.view == viewChat {
			styleTextarea(&a.chat.textarea)
			a.chat.refreshViewport()
		}
		return a, nil

	case openModelPickerMsg:
		a.prevView = a.view
		a.models = newModelPicker(a.client.ModelRaw())
		a.models.setSize(a.width, a.height)
		a.view = viewModels
		return a, nil

	case openThemePickerMsg:
		a.prevView = a.view
		a.themes = newThemePicker()
		a.themes.setSize(a.width, a.height)
		a.view = viewThemes
		return a, nil

	case cycleAppearanceMsg:
		// auto → light → dark → auto
		next := "light"
		switch AppearanceMode() {
		case "":
			next = "light"
		case "light":
			next = "dark"
		case "dark":
			next = ""
		}
		SetAppearanceMode(next)
		cur, _ := storage.LoadConfig()
		cur.Appearance = next
		_ = storage.SaveConfig(cur)
		label := next
		if label == "" {
			label = "auto"
		}
		if a.view == viewChat {
			a.chat.notice = "appearance: " + label
			styleTextarea(&a.chat.textarea)
			a.chat.refreshViewport()
		}
		return a, nil

	case modelPickedMsg:
		a.client.SetModel(msg.alias)
		cur, _ := storage.LoadConfig()
		cur.Model = msg.alias
		_ = storage.SaveConfig(cur)
		a.view = a.prevView
		if a.view == viewChat {
			a.chat.refreshViewport()
		}
		return a, nil

	case modelCanceledMsg:
		a.view = a.prevView
		return a, nil

	case openCommandPaletteMsg:
		a.prevView = a.view
		a.palette = newCommandPalette(a.view == viewChat)
		a.palette.setSize(a.width, a.height)
		a.view = viewPalette
		return a, a.palette.Init()

	case commandPaletteCanceledMsg:
		a.view = a.prevView
		return a, nil

	case commandPalettePickedMsg:
		a.view = a.prevView
		return a, a.runCommand(msg.id)

	case openRenameMsg:
		a.prevView = a.view
		a.rename = newRenameDialog(msg.sessionID, msg.current)
		a.rename.setSize(a.width, a.height)
		a.view = viewRename
		return a, a.rename.Init()

	case renameCanceledMsg:
		a.view = a.prevView
		return a, nil

	case renameAppliedMsg:
		a.view = a.prevView
		if sess, err := a.store.Load(msg.sessionID); err == nil {
			sess.Name = msg.name
			_ = a.store.Save(sess)
			// Reflect the new name in the live chat session if it's the open one.
			if a.view == viewChat && a.chat.session != nil && a.chat.session.ID == msg.sessionID {
				a.chat.session.Name = msg.name
			}
		}
		if a.view == viewSessions {
			return a, loadSessions(a.store)
		}
		return a, nil
	}

	switch a.view {
	case viewSessions:
		updated, cmd := a.sessions.Update(msg)
		a.sessions = updated
		return a, cmd
	case viewChat:
		updated, cmd := a.chat.Update(msg)
		a.chat = updated
		return a, cmd
	case viewThemes:
		updated, cmd := a.themes.Update(msg)
		a.themes = updated
		return a, cmd
	case viewModels:
		updated, cmd := a.models.Update(msg)
		a.models = updated
		return a, cmd
	case viewPalette:
		updated, cmd := a.palette.Update(msg)
		a.palette = updated
		return a, cmd
	case viewRename:
		updated, cmd := a.rename.Update(msg)
		a.rename = updated
		return a, cmd
	}

	return a, nil
}

// runCommand turns a palette pick into the command that emits the matching
// message. The palette has already restored a.view to the previous one, so
// the resulting message lands in the same view it was opened from.
func (a *App) runCommand(id commandID) tea.Cmd {
	switch id {
	case cmdChangeTheme:
		return func() tea.Msg { return openThemePickerMsg{} }
	case cmdChangeModel:
		return func() tea.Msg { return openModelPickerMsg{} }
	case cmdToggleAppearance:
		return func() tea.Msg { return cycleAppearanceMsg{} }
	case cmdRenameSession:
		// Figure out which session to rename: open one in chat, selected one
		// on the sessions list. No-op if there's nothing to rename.
		if a.view == viewChat && a.chat.session != nil {
			id := a.chat.session.ID
			name := a.chat.session.Name
			return func() tea.Msg { return openRenameMsg{sessionID: id, current: name} }
		}
		if a.view == viewSessions && len(a.sessions.sessions) > 0 {
			sess := a.sessions.sessions[a.sessions.cursor]
			id := sess.ID
			name := sess.Name
			return func() tea.Msg { return openRenameMsg{sessionID: id, current: name} }
		}
		return nil
	case cmdNewSession:
		return func() tea.Msg { return newSessionMsg{} }
	case cmdClearSession:
		return func() tea.Msg { return clearSessionMsg{} }
	case cmdBackToSessions:
		return func() tea.Msg { return backMsg{} }
	}
	return nil
}

func (a *App) View() tea.View {
	var content string
	if a.initErr != nil {
		content = s.Error.Render("failed to initialize: " + a.initErr.Error() + "\n  press any key to quit")
	} else {
		switch a.view {
		case viewSessions:
			content = a.sessions.View()
		case viewChat:
			content = a.chat.View()
		case viewThemes:
			content = a.themes.View()
		case viewModels:
			content = a.models.View()
		case viewPalette:
			content = a.palette.View()
		case viewRename:
			content = a.rename.View()
		}
	}
	v := tea.NewView(content)
	v.AltScreen = true
	// Leave mouse mode off so terminals (Ghostty, iTerm2, …) keep native
	// drag-select on the rendered output. Enabling CellMotion captures the
	// drag and forces the user to hold ⌥/Shift to select text.
	v.MouseMode = tea.MouseModeNone
	return v
}

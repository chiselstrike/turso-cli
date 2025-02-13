package prompt

import (
	"fmt"
	"os"

	spn "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type SpinnerT struct {
	spinner   spn.Model
	prefix    string
	suffix    string
	quitting  bool
	cancelled bool
	done      chan bool
}

func newSpinner(prefix, suffix string) *SpinnerT {
	s := spn.New()
	s.Spinner = spn.Dot
	return &SpinnerT{spinner: s, prefix: prefix, suffix: suffix}
}

func (m *SpinnerT) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *SpinnerT) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting, m.cancelled = true, true
			return m, tea.Quit
		}
		return m, nil
	case error:
		return m, nil
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m *SpinnerT) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("%s%s %s", m.prefix, m.spinner.View(), m.suffix)
}

func (m *SpinnerT) Stop() {
	m.quitting = true
	if m.done != nil {
		<-m.done
	}
}

func (m *SpinnerT) Text(t string) {
	m.suffix = t
}

func (m *SpinnerT) Start() {
	if !isInteractive {
		fmt.Println(m.View())
		return
	}

	ch := make(chan bool)
	m.done = ch
	m.quitting = false
	m.cancelled = false
	go func() {
		defer close(ch)
		tea.NewProgram(m).Run()
		if m.cancelled {
			os.Exit(130)
		}
	}()
}

func StoppedSpinner(text string) *SpinnerT {
	spinner := newSpinner("", text)
	return spinner
}

func Spinner(text string) *SpinnerT {
	spinner := StoppedSpinner(text)
	spinner.Start()
	return spinner
}

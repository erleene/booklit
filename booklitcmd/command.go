package booklitcmd

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"

	"github.com/sirupsen/logrus"
	"github.com/vito/booklit"
	"github.com/vito/booklit/baselit"
	"github.com/vito/booklit/load"
	"github.com/vito/booklit/render"
)

type Command struct {
	Version func() `short:"v" long:"version" description:"Print the version of Boooklit and exit."`

	In  string `long:"in"  short:"i" required:"true" description:"Input .lit file."`
	Out string `long:"out" short:"o" required:"true" description:"Output directory in which to render."`

	SaveSearchIndex bool `long:"save-search-index" description:"Save a search index JSON file in the destination."`

	ServerPort int `long:"serve" short:"s" description:"Start an HTTP server on the given port."`

	Plugins []string `long:"plugin" short:"p" description:"Package to import, providing a plugin."`

	Debug bool `long:"debug" short:"d" description:"Log at debug level."`

	AllowBrokenReferences bool `long:"allow-broken-references" description:"Replace broken references with a bogus tag."`

	HTMLEngine struct {
		Templates string `long:"templates" description:"Directory containing .tmpl files to load."`
	} `group:"HTML Rendering Engine" namespace:"html"`

	processor *load.Processor
	engine    *render.HTMLRenderingEngine
	writer    render.Writer
}

func (cmd *Command) Execute(args []string) error {
	if cmd.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	err := cmd.loadPlugins()
	if err != nil {
		return err
	}

	cmd.processor = &load.Processor{
		AllowBrokenReferences: cmd.AllowBrokenReferences,
	}

	cmd.engine = render.NewHTMLRenderingEngine()

	cmd.writer = render.Writer{
		Engine:      cmd.engine,
		Destination: cmd.Out,
	}

	if cmd.ServerPort != 0 {
		return cmd.Serve()
	} else {
		return cmd.Build()
	}
}

func (cmd *Command) Serve() error {
	http.Handle("/", &Server{
		Command:    cmd,
		FileServer: http.FileServer(http.Dir(cmd.Out)),
	})

	return http.ListenAndServe(fmt.Sprintf(":%d", cmd.ServerPort), nil)
}

func (cmd *Command) Build() error {
	pluginFactories := []booklit.PluginFactory{
		baselit.NewPlugin,
	}

	section, err := cmd.processor.LoadFile(cmd.In, pluginFactories)
	if err != nil {
		return err
	}

	err = os.MkdirAll(cmd.Out, 0755)
	if err != nil {
		return err
	}

	if cmd.HTMLEngine.Templates != "" {
		err := cmd.engine.LoadTemplates(cmd.HTMLEngine.Templates)
		if err != nil {
			return err
		}
	}

	err = cmd.writer.WriteSection(section)
	if err != nil {
		return err
	}

	if cmd.SaveSearchIndex {
		err = cmd.writer.WriteSearchIndex(section)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cmd *Command) loadPlugins() error {
	tmpdir, err := ioutil.TempDir("", "booklit-reexec")
	if err != nil {
		return err
	}

	defer func() {
		_ = os.RemoveAll(tmpdir)
	}()

	for i, p := range cmd.Plugins {
		logrus.Infoln("loading plugin", p)

		pluginPath := filepath.Join(tmpdir, fmt.Sprintf("plugin-%d.so", i))

		build := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, p)
		build.Env = append(os.Environ(), "GOBIN="+tmpdir)
		buildOutput, err := build.CombinedOutput()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("failed to compile plugin '%s':\n\n%s", p, string(buildOutput))
			} else {
				return err
			}
		}

		_, err = plugin.Open(pluginPath)
		if err != nil {
			return fmt.Errorf("failed to load plugin '%s': %s", p, err)
		}
	}

	return nil
}

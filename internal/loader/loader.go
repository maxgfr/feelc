// Package loader lit les sources .rules, applique le pipeline COMPILE-VERIFY-THEN-SWAP,
// et surveille le fichier (fsnotify + debounce) pour le hot-reload.
//
// RÈGLE D'OR : on ne publie JAMAIS un modèle invalide. Une source qui ne compile pas (ou, en
// mode strict, qui a des bloqueurs de vérification) laisse le service sur l'ancien modèle sain.
package loader

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/verify"
)

// Compile parse + compile + vérifie une source (sans nom de fichier).
func Compile(src []byte) (*ir.CompiledModel, string, *verify.Report, error) {
	return CompileFile("", src)
}

// CompileFile est Compile avec un nom de fichier propagé sur les erreurs structurées
// (diag.Error.File), pour des diagnostics « file:line:col: ... » exploitables.
func CompileFile(path string, src []byte) (*ir.CompiledModel, string, *verify.Report, error) {
	m, err := dsl.ParseFile(path, string(src))
	if err != nil {
		return nil, "", nil, err // déjà stampé par ParseFile
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		return nil, "", nil, diag.WithFileIfDiag(err, path)
	}
	rep := verify.Verify(cm)
	// Hash = identité CANONIQUE du modèle compilé (hex(ir.Hash)), pas du texte source :
	// deux sources qui compilent vers le même IR partagent le hash (breaking voulu, ADR 0006).
	h, err := ir.Hash(cm)
	if err != nil {
		return nil, "", nil, err
	}
	return cm, hex.EncodeToString(h[:]), rep, nil
}

// Reload lit un fichier et le publie dans reg SI valide. En mode strict, des bloqueurs de
// vérification empêchent la publication. Sur erreur, le modèle courant est conservé.
func Reload(path string, reg *registry.Registry, strict bool) (*registry.Entry, *verify.Report, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	cm, hash, rep, err := CompileFile(path, src)
	if err != nil {
		return nil, nil, err // erreur de compilation -> pas de swap
	}
	if strict && rep.Blockers() > 0 {
		return nil, rep, fmt.Errorf("%d bloqueur(s) de vérification (mode strict) — modèle non publié", rep.Blockers())
	}
	return reg.StoreWithSource(cm, hash, src), rep, nil
}

// Watch surveille le fichier (via son répertoire, pour survivre aux write-rename des éditeurs)
// et déclenche Reload après un debounce. Renvoie une fonction d'arrêt.
func Watch(path string, reg *registry.Registry, strict bool, onReload func(*registry.Entry, *verify.Report, error)) (func() error, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Add(filepath.Dir(abs)); err != nil {
		_ = w.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		debounce := func() {
			e, rep, err := Reload(path, reg, strict)
			if onReload != nil {
				onReload(e, rep, err)
			}
		}
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if evAbs, _ := filepath.Abs(ev.Name); evAbs != abs {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(150*time.Millisecond, debounce)
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() error {
		close(done)
		return w.Close()
	}, nil
}

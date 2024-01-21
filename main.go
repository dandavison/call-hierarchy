package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"call-hierarchy/d2"
	"call-hierarchy/gopls"
)

type Counter struct {
	lock  sync.Mutex
	count int
}

func (c *Counter) incr() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.count += 1
}

func (c *Counter) decr() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.count -= 1
}

// BuildGraph builds a graph by following edges from a function definition f to
// other functions definitions containing call sites of f.
func BuildGraph(f gopls.Function) (d2.Graph, error) {
	maxLineages := 20
	tasks := make(chan gopls.Function, maxLineages)
	errors := make(chan error)
	done := make(chan struct{})
	counter := Counter{}
	visited := map[gopls.Function]bool{}

	tasks <- f
	visited[f] = true
	counter.count = 1
	g := d2.Graph{}
	for {
		select {
		case f := <-tasks:
			go func(f gopls.Function) {
				fmt.Fprintf(
					os.Stderr,
					"visiting %s:%s\n",
					f.Name,
					f.Location.String(),
				)
				defer func() {
					if counter.count >= maxLineages {
						fmt.Fprintf(os.Stderr, "Reached max number of lineages (%d)\n", maxLineages)
						close(done)
					}
					counter.decr()
					if counter.count == 0 {
						close(done)
					}
				}()
				node, err := gopls.CallHierarchy(f.LocationString())
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return
				}
				for _, callSite := range node.Callers {
					if callSite.Function.IsTest() {
						continue
					}
					g.Edges = append(g.Edges, d2.Edge{From: callSite, To: f})
					if !visited[callSite.Function] {
						counter.incr()
						visited[callSite.Function] = true
						tasks <- callSite.Function
					}
				}
			}(f)
		case err := <-errors:
			return d2.Graph{}, err
		case <-done:
			return g, nil
		}
	}
}

func main() {
	name := os.Args[1]
	file := os.Args[2]
	linkPrefix := strings.TrimSuffix(os.Args[3], "/")
	function, err := gopls.GetFunction(name, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	g, err := BuildGraph(function)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	g.Write(linkPrefix)
	if false {
		gopls.Test()
		os.Exit(0)
	}
}

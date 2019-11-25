package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func outputWorld(p golParams, d distributorChans, world [][]byte, turns int) {
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight) + "-" + strconv.Itoa(turns)}, "x")

	// Sends the world to the pgm channel
	for y := range world {
		for x := range world[y]{
			d.io.outputVal <- world[y][x]
		}
	}
}

func modPos(d, m int) int {
	var res = d % m
	if (res < 0 && m > 0) || (res > 0 && m < 0) {
		return res + m
	}
	return res
}

func worker(startY, endY int, p golParams, out chan<- byte, in <-chan byte) {

	smallWorldHeight := endY-startY+2

	smallWorld := make([][]byte, smallWorldHeight)
	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
	}

	for {

		for y := 0; y < smallWorldHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {
				smallWorld[y][x] = <- in
			}
		}

		for y := 1; y < endY-startY+1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				alive := 0
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {
						if (i != 0 || j != 0) && smallWorld[((y+i)+smallWorldHeight)%smallWorldHeight][((x+j)+p.imageWidth)%p.imageWidth] != 0 {
							alive++
						}
					}
				}
				if smallWorld[y][x] != 0 {
					if alive < 2 || alive > 3 {
						out <- smallWorld[y][x] ^ 0xFF
					} else {
						out <- smallWorld[y][x]
					}
				} else {
					if alive == 3 {
						out <- smallWorld[y][x] ^ 0xFF
					} else {
						out <- smallWorld[y][x]
					}
				}
			}
		}
	}
}

func eventController(keyChan <- chan rune, p golParams, d distributorChans, world[][]byte, turns *int, isPause chan bool) {
	for {
		select {
		case i := <-keyChan:
			if i == 's' {
				outputWorld(p, d, world, *turns)
			} else if i == 'p' {
				isPause <- true
			} else if i == 'q' {
				outputWorld(p, d, world, *turns)
				StopControlServer()
				os.Exit(0)
			}

		}
	}
}

func pauseGame(turns int) {
	fmt.Println("Current turn in execution: ", turns)
	bufio.NewReader(os.Stdin).ReadBytes('p')
	fmt.Println("Continuing.")
}


// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, keyChan <-chan rune) {

	// Create the 2D slice to store the world.
	world := make([][]byte, p.imageHeight)
	for i := range world {
		world[i] = make([]byte, p.imageWidth)
	}

	// Request the io goroutine to read in the image with the given filename.
	d.io.command <- ioInput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// The io goroutine sends the requested image byte by byte, in rows.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			val := <-d.io.inputVal
			if val != 0 {
				fmt.Println("Alive cell at", x, y)
				world[y][x] = val
			}
		}
	}

	//Height the worker will work on
	workerHeight := p.imageHeight / p.threads

	//Array of channels intended for workers
	out := make([]chan byte, p.threads)
	in := make([]chan byte, p.threads)

	for i := range out {
		out[i] = make(chan byte)
	}

	for i := range in {
		in[i] = make(chan byte)
	}

	for i := 0; i < p.threads; i++ {
		go worker(i*workerHeight, (i+1)*workerHeight, p, out[i], in[i])
	}

	turns := 0
	isPause := make(chan bool)
	go eventController(keyChan, p, d, world, &turns, isPause)

	// Calculate the new state of Game of Life after the given number of turns.
	for turns = 0; turns < p.turns; turns++ {

		select {
		case b := <- isPause:
			if b {
				pauseGame(turns)
			}
		default:
		}

		//Sends world byte by byte to workers
		for t := 0; t < p.threads; t++ {
			for y := 0; y < workerHeight+2; y++ {
				for x := 0; x < p.imageWidth; x++ {
					in[t] <- world[modPos(y + (t*(workerHeight)-1), p.imageHeight)][x]
				}
			}
		}

		//Receives world byte by byte from workers
		for t := 0; t < p.threads; t++ {
			for y := 0; y < workerHeight; y++ {
				for x := 0; x < p.imageWidth; x++ {
					world[y+(t*workerHeight)][x] = <- out[t]
				}
			}

		}

	}

	outputWorld(p, d, world, p.turns)

	// Create an empty slice to store coordinates of cells that are still alive after p.turns are done.
	var finalAlive []cell
	// Go through the world and append the cells that are still alive.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			if world[y][x] != 0 {
				finalAlive = append(finalAlive, cell{x: x, y: y})
			}
		}
	}

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}
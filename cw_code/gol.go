package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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
	if d >= 0 {
		return d % m
	}
	return d + m
}

func worker(startY, endY int, p golParams, out chan<- byte, in <-chan byte) {

	smallWorldHeight := endY-startY+2

	smallWorld := make([][]byte, smallWorldHeight)
	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
	}

	for {
		//Creates new small world with halos
		for y := 0; y < smallWorldHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {
				smallWorld[y][x] = <- in
			}
		}

		//Counts number of alive neighbours for each cell
		for y := 1; y < endY-startY+1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				alive := 0
				alive = int(smallWorld[modPos((y-1) ,smallWorldHeight)][modPos((x-1) ,p.imageWidth)]) + int(smallWorld[modPos((y-1), smallWorldHeight)][modPos((x), p.imageWidth)]) + int(smallWorld[modPos((y-1), smallWorldHeight)][modPos((x+1), p.imageWidth)]) +
					int(smallWorld[modPos((y), smallWorldHeight)][modPos((x-1), p.imageWidth)])                        +                              int(smallWorld[(y) % smallWorldHeight][(x+1) % p.imageWidth])           +
					int(smallWorld[modPos((y+1), smallWorldHeight)][modPos((x-1), p.imageWidth)]) +     int(smallWorld[(y+1) % smallWorldHeight][(x) % p.imageWidth])     + int(smallWorld[(y+1) % smallWorldHeight][(x+1) % p.imageWidth])
				alive /= 255

				//Flips cell or sends back original if no change was made
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

func eventController(keyChan <- chan rune, p golParams, d distributorChans, world[][]byte, turns *int) {
	for {
		timePrint := time.After(2 * time.Second)
		select {
		case i := <-keyChan:
			if i == 's' {
				outputWorld(p, d, world, *turns)
			} else if i == 'p' {
				turns2 := *turns
				fmt.Println("Game paused, current turn: ", strconv.Itoa(turns2))

				for x:= true; x == true; {
					select {
					case resume := <- keyChan:
						if resume == 'p' {
							fmt.Println("Continuing.")
							x = false
						}
					}
				}

			} else if i == 'q' {
				outputWorld(p, d, world, *turns)
				StopControlServer()
				os.Exit(0)
			}
		case <- timePrint:
			count := 0
			for y := 0; y < p.imageHeight; y++ {
				for x := 0; x < p.imageWidth; x++ {
					if world[y][x] != 0 {
						count++
					}
				}
			}
			fmt.Println("Alive cells: " + strconv.Itoa(count))

		}
	}
}


// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, keyChan <-chan rune, in, out []chan byte) {

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
	workerHeightRemainder := 0

	//Checks if the threads are not a power of 2
	workerHeightRemainder = p.imageHeight % p.threads

	turns := 0
	go eventController(keyChan, p, d, world, &turns)

	// Calculate the new state of Game of Life after the given number of turns.
	for turns = 0; turns < p.turns; turns++ {
			//Sends world byte by byte to workers
			for t := 0; t < p.threads-1; t++ {
				for y := 0; y < workerHeight+2; y++ {
					for x := 0; x < p.imageWidth; x++ {
						in[t] <- world[modPos(y+(t*(workerHeight)-1), p.imageHeight)][x]
					}
				}
			}
			for y:=0; y<workerHeight+workerHeightRemainder+2; y++ {
				for x := 0; x < p.imageWidth; x++ {
					in[p.threads-1] <- world[modPos(y+((p.threads-1)*(workerHeight)-1), p.imageHeight)][x]
				}
			}
			//Receives world byte by byte from workers
			for t := 0; t < p.threads-1; t++ {
				for y := 0; y < workerHeight; y++ {
					for x := 0; x < p.imageWidth; x++ {
						world[y+(t*workerHeight)][x] = <-out[t]
					}
				}
			}
			for y:=0; y<workerHeight+workerHeightRemainder; y++ {
				for x := 0; x < p.imageWidth; x++ {
					world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
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
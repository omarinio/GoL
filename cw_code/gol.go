package main

import (
	"fmt"
	"strconv"
	"strings"
)

func outputWorld(p golParams, d distributorChans, world [][]byte) {
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight) + "-" + strconv.Itoa(p.turns)}, "x")

	// Sends the world to the pgm channel
	for y := range world {
		for x := range world[y]{
			d.io.outputVal <- world[y][x]
		}
	}
}

func worker(startY, endY int, p golParams, out chan<- byte, in <-chan byte) {

	smallWorld := make([][]byte, endY-startY+2)
	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
	}



	for y := startY; y < endY; y++ {
		for x := 0; x < p.imageWidth; x++ {
			alive := 0
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {
					if (i != 0 || j != 0) && world2[((y+i)+p.imageHeight)%p.imageHeight][((x+j)+p.imageWidth)%p.imageWidth] != 0 {
						alive++
					}
				}
			}
			if world2[y][x] != 0 {
				if alive < 2 || alive > 3 {
					out <-  world[y][x] ^ 0xFF
				}
			} else {
				if alive == 3 {
					out <- world[y][x] ^ 0xFF
				}
			}
		}
	}
}


// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell) {

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

	//Copy of world
	world2 := make([][]byte, len(world))
	for i := range world {
		world2[i] = make([]byte, len(world[i]))
		copy(world2[i], world[i])
	}

	workerHeight := p.imageHeight / p.threads
	//Array of channels intended for workers
	out := make([]chan byte, p.threads)
	in := make([]chan byte, p.threads)

	for i := range out {
		out[i] = make(chan byte)
	}

	for i := range out {
		in[i] = make(chan byte)
	}

	for i := 0; i < p.threads; i++ {
		go worker(i*workerHeight, (i+1)*workerHeight, p, out[i], in[i])
	}

	// Calculate the new state of Game of Life after the given number of turns.
	for turns := 0; turns < p.turns; turns++ {
		//Sends world byte by byte to workers
		for t := 0; t < p.threads; p++ {
			for y := workerHeight*t - 1; y < workerHeight*t + 1; y++ {
				for x := 0; x < p.imageWidth; x++ {
					in[t] <- world[t*(y%p.imageHeight)][x]
				}
			}

		}


		for y:= 0; y < p.imageHeight; y++ {
			for x:= 0; x < p.imageWidth; x++ {

			}
		}


		for i := range world {
			world2[i] = make([]byte, len(world[i]))

			//copy(world2[i], world[i])
		}
	}

	outputWorld(p, d, world)

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
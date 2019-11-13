package main

import (
	"fmt"
	"strconv"
	"strings"
)

//Sends world to pgm channel
func outputWorld(p golParams, d distributorChans, world [][]byte) {
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight) + "-" + strconv.Itoa(p.turns)}, "x")

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
	smallWorld := make([][]byte, endY-startY+2)
	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
	}

	//Sets height of slice to be equivalent to image height/num of workers
	smallWorldHeight := endY-startY+2

	//Infinite loop
	for {
		for y := 0; y < endY-startY+2; y++ {
			for x := 0; x < p.imageWidth; x++ {
				smallWorld[y][x] = <- in
			}
		}

		//Updating cells logic
		for y := 1; y < endY-startY+1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				//Alive neighbours counter
				alive := 0
				for i := -1; i <= 1; i++ {
					for j := -1; j <= 1; j++ {
						if (i != 0 || j != 0) && smallWorld[((y+i)+smallWorldHeight)%smallWorldHeight][((x+j)+p.imageWidth)%p.imageWidth] != 0 {
							alive++
						}
					}
				}
				//Changing state of cells based on game rules
				if smallWorld[y][x] != 0 {
					if alive < 2 || alive > 3 {
						out <- smallWorld[y][x] ^ 0xFF
					} else {
						//Push unchanged cells to 'out' channel
						out <- smallWorld[y][x]
					}
				} else {
					if alive == 3 {
						out <- smallWorld[y][x] ^ 0xFF
					} else {
						//Push unchanged cells to 'out' channel
						out <- smallWorld[y][x]
					}
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

	//Height the worker will work on
	workerHeight := p.imageHeight / p.threads

	//Array of channels intended for workers
	out := make([]chan byte, p.threads)
	in := make([]chan byte, p.threads)

	//Create separate 'out' channels for each worker
	for i := range out {
		out[i] = make(chan byte)
	}

	//Create separate 'in' channels for each worker
	for i := range in {
		in[i] = make(chan byte)
	}

	//Start all worker goroutines
	for i := 0; i < p.threads; i++ {
		go worker(i*workerHeight, (i+1)*workerHeight, p, out[i], in[i])
	}

	// Calculate the new state of Game of Life after the given number of turns.
	for turns := 0; turns < p.turns; turns++ {
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

	//Output world to pgm file using 'outputWorld' function
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
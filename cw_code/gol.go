package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type workerChannel struct {
	upperSend chan<-byte //halo for worker above is sent
	upperRec <-chan byte //halo above is received from worker
	lowerSend chan<-byte //halo for worker below is sent
	lowerRec <-chan byte //halo below is received from worker

	parityBit <-chan bool //tells workers when to output the world
}

func outputWorld(p golParams, d distributorChans, world [][]byte, turns int) {
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight) + "-" + strconv.Itoa(turns)}, "x")

	// Sends the world to the pgm channel
	for y := range world {
		for x := range world[y] {
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

func worker(startY, endY int, p golParams, out chan<- byte, in <-chan byte, wc workerChannel) {

	smallWorldHeight := endY-startY+2

	smallWorld := make([][]byte, smallWorldHeight)
	tempSmallWorld := make([][]byte, smallWorldHeight)

	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
		tempSmallWorld[i] = make([]byte, p.imageWidth)
	}

	//Creates new small world with halos
	for y := 0; y < smallWorldHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			smallWorld[y][x] = <- in
		}
	}

	for turn:= 0; turn < p.turns; turn++ {

		//At beginning of each turn, must send and receive halos
		//Sending halos
		for halo := 0; halo < p.imageWidth; halo++ {
			wc.upperSend <- smallWorld[1][halo]
			wc.lowerSend <- smallWorld[smallWorldHeight-2][halo]
		}

		//Receiving halos (receive from distributor)
		for halo := 0; halo < p.imageWidth; halo++ {
			smallWorld[0][halo] = <- wc.upperRec
			smallWorld[smallWorldHeight-1][halo] = <- wc.lowerRec
		}

		//Counts number of alive neighbours for each cell
		for y := 1; y < endY-startY+1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				alive := 0
				alive = int(smallWorld[modPos(y-1 ,smallWorldHeight)][modPos(x-1 ,p.imageWidth)]) + int(smallWorld[modPos(y-1, smallWorldHeight)][modPos(x, p.imageWidth)]) + int(smallWorld[modPos(y-1, smallWorldHeight)][modPos(x+1, p.imageWidth)]) +
					int(smallWorld[modPos(y, smallWorldHeight)][modPos(x-1, p.imageWidth)])                        +                              int(smallWorld[(y) % smallWorldHeight][(x+1) % p.imageWidth])           +
					int(smallWorld[modPos(y+1, smallWorldHeight)][modPos(x-1, p.imageWidth)]) +     int(smallWorld[(y+1) % smallWorldHeight][(x) % p.imageWidth])     + int(smallWorld[(y+1) % smallWorldHeight][(x+1) % p.imageWidth])
				alive = alive/255

				//Flips cell or sends back original if no change was made
				if smallWorld[y][x] != 0 {
					if alive < 2 || alive > 3 {
						tempSmallWorld[y][x] = smallWorld[y][x] ^ 0xFF
					} else {
						tempSmallWorld[y][x] = smallWorld[y][x]
					}
				} else {
					if alive == 3 {
						tempSmallWorld[y][x] = smallWorld[y][x] ^ 0xFF
					} else {
						tempSmallWorld[y][x] = smallWorld[y][x]
					}
				}
			}
		}

		//Copy temp world into real world
		for i := range smallWorld {
			smallWorld[i] = make([]byte, len(tempSmallWorld[i]))
			copy(smallWorld[i], tempSmallWorld[i])
		}
	}

	//Sends the world back once all turns are finished
	for y := 1; y < endY-startY+1; y++ {
		for x := 0; x < p.imageWidth; x++ {
			out <- smallWorld[y][x]
		}
	}

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
	workerHeightRemainder := 0

	//Checks if the threads are not a power of 2
	workerHeightRemainder = p.imageHeight % p.threads

	//Channels to send halos to and from worker
	sendChans := make([]chan<- byte, 2 * p.threads)
	recChans := make([]<-chan byte, 2 * p.threads)
	parityBitChans := make([]chan bool, p.threads)

	//Array of channels intended for workers
	out := make([]chan byte, p.threads)
	in := make([]chan byte, p.threads)

	for i:=0; i < 2 * p.threads; i++ {
		c := make(chan byte, 2 * p.imageWidth)
		recChans[i] = c
		sendChans[i] = c
	}

	for i := range out {
		out[i] = make(chan byte, p.imageWidth)
	}

	for i := range in {
		in[i] = make(chan byte, p.imageWidth)
	}

	for i := range parityBitChans {
		parityBitChans[i] = make(chan bool)
	}

	for i := 0; i < p.threads-1; i++ {
		var wc workerChannel

		wc.upperSend = sendChans[i*2]
		wc.upperRec = recChans[modPos((i*2)-1, 2*p.threads)]
		wc.lowerSend = sendChans[(i*2)+1]
		wc.lowerRec = recChans[modPos((i+1)*2, 2*p.threads)]
		wc.parityBit = parityBitChans[i]


		go worker(i*workerHeight, (i+1)*workerHeight, p, out[i], in[i], wc)

		for y := 0; y < workerHeight+2; y++ {
			for x := 0; x < p.imageWidth; x++ {
				in[i] <- world[modPos(y+(i*(workerHeight)-1), p.imageHeight)][x]
			}
		}
	}

	var wc2 workerChannel

	wc2.upperSend = sendChans[(p.threads-1)*2]
	wc2.upperRec = recChans[modPos(((p.threads-1)*2)-1, 2*p.threads)]
	wc2.lowerSend = sendChans[((p.threads-1)*2)+1]
	wc2.lowerRec = recChans[modPos(((p.threads-1)+1)*2, 2*p.threads)]
	wc2.parityBit = parityBitChans[p.threads-1]

	go worker((p.threads-1)*workerHeight, ((p.threads)*workerHeight)+workerHeightRemainder, p, out[p.threads-1], in[p.threads-1], wc2)

	for y := 0; y < workerHeight+workerHeightRemainder+2; y++ {
		for x := 0; x < p.imageWidth; x++ {
			in[p.threads-1] <- world[modPos(y+((p.threads-1)*(workerHeight)-1), p.imageHeight)][x]
		}
	}

	turns := 0
	//go eventController(keyChan, p, d, world, &turns)
	timeAfter := time.NewTicker(time.Second * 2)

	// Calculate the new state of Game of Life after the given number of turns.
	for turns = 0; turns < p.turns; turns++ {
		select {
		case <-timeAfter.C:
			for _, parityBitChan := range parityBitChans {
				parityBitChan <- true
			}

			alive := 0

			for t := 0; t < p.threads-1; t++ {
				for y := 0; y < workerHeight; y++ {
					for x := 0; x < p.imageWidth; x++ {
						alive += int(<-out[t])
					}
				}
			}
			for y := 0; y < workerHeight+workerHeightRemainder; y++ {
				for x := 0; x < p.imageWidth; x++ {
					alive += int(<-out[p.threads-1])
				}
			}

			alive = alive / 255

			fmt.Println("Alive cells: " + strconv.Itoa(alive))

		case i := <-keyChan:
			if i == 's' {
				for _, parityBitChan := range parityBitChans {
					parityBitChan <- true
				}

				for t := 0; t < p.threads; t++ {
					for y := 0; y < workerHeight; y++ {
						for x := 0; x < p.imageWidth; x++ {
							world[y+(t*workerHeight)][x] =<- out[t]
						}
					}
				}

				for y := 0; y < workerHeight+workerHeightRemainder; y++ {
					for x := 0; x < p.imageWidth; x++ {
						world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
					}
				}

				outputWorld(p, d, world, turns)

			} else if i == 'p' {
				fmt.Println("Game paused, current turn: ", strconv.Itoa(turns))

				for x := true; x == true; {
					select {
					case resume := <-keyChan:
						if resume == 'p' {
							fmt.Println("Continuing.")
							x = false
						}
					}
				}

			} else if i == 'q' {
				for _, parityBitChan := range parityBitChans {
					parityBitChan <- true
				}

				for t := 0; t < p.threads; t++ {
					for y := 0; y < workerHeight; y++ {
						for x := 0; x < p.imageWidth; x++ {
							world[y+(t*workerHeight)][x] =<- out[t]
						}
					}
				}

				for y := 0; y < workerHeight+workerHeightRemainder; y++ {
					for x := 0; x < p.imageWidth; x++ {
						world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
					}
				}

				outputWorld(p, d, world, turns)
				StopControlServer()
				os.Exit(0)
			}
			default:
		}
	}

	for t := 0; t < p.threads-1; t++ {
		for y := 0; y < workerHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {
				world[y+(t*workerHeight)][x] = <- out[t]
			}
		}
	}

	for y := 0; y < workerHeight+workerHeightRemainder; y++ {
		for x := 0; x < p.imageWidth; x++ {
			world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
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
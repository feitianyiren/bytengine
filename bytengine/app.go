package main

import (
	"fmt"
	"io/ioutil"
	"os"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/codegangsta/cli"
	"github.com/gin-gonic/gin"
	"github.com/johnwilson/bytengine/bfs"
	"github.com/johnwilson/bytengine/bytengine/core"
	"github.com/johnwilson/bytengine/core"
	"github.com/johnwilson/bytengine/dsl"
)

var ScriptsChan chan *core.ScriptRequest
var CommandsChan chan *core.CommandRequest

func runScriptHandler(ctx *gin.Context) {
	var form struct {
		Token string `form:"token" binding:"required"`
		Query string `form:"query" binding:"required"`
	}

	ok := ctx.Bind(&form)
	if !ok {
		data := bfs.ErrorResponse(fmt.Errorf("Missing parameters")).JSON()
		ctx.Data(400, "application/json", data)
		return
	}

	q := &core.ScriptRequest{form.Query, form.Token, make(chan []byte)}
	ScriptsChan <- q
	data := <-q.ResultChannel
	ctx.Data(200, "application/json", data)
}

func getTokenHandler(ctx *gin.Context) {
	var form struct {
		Username string `form:"username" binding:"required"`
		Password string `form:"password" binding:"required"`
	}
	ok := ctx.Bind(&form)
	if !ok {
		data := bfs.ErrorResponse(fmt.Errorf("Missing parameters")).JSON()
		ctx.Data(400, "application/json", data)
		return
	}

	cmd := dsl.NewCommand("login", false)
	cmd.Args["username"] = form.Username
	cmd.Args["password"] = form.Password
	c := &core.CommandRequest{cmd, "", make(chan bfs.BFSResponse)}
	CommandsChan <- c
	data := <-c.ResultChannel
	ctx.Data(200, "application/json", data.JSON())
}

func getUploadTicketHandler(ctx *gin.Context) {
	var form struct {
		Token    string `form:"token" binding:"required"`
		Database string `form:"database" binding:"required"`
		Path     string `form:"path" binding:"required"`
	}
	ok := ctx.Bind(&form)
	if !ok {
		data := bfs.ErrorResponse(fmt.Errorf("Missing parameters")).JSON()
		ctx.Data(400, "application/json", data)
		return
	}

	cmd := dsl.NewCommand("uploadticket", false)
	cmd.Database = form.Database
	cmd.Args["path"] = form.Path
	c := &core.CommandRequest{cmd, form.Token, make(chan bfs.BFSResponse)}
	CommandsChan <- c
	data := <-c.ResultChannel
	ctx.Data(200, "application/json", data.JSON())
}

func uploadFileHelper(max int, ctx *gin.Context) (string, int, error) {
	total := 0             // total bytes read/written
	maxbytes := 1024 * max // maximum upload size in bytes

	tmpfile, err := ioutil.TempFile("", "bytengine_upload_") // upload file name from header
	if err != nil {
		return "", 0, err
	}
	defer tmpfile.Close()

	// create read buffer
	var bsize int64 = 16 * 1024 // 16 kb
	buffer := make([]byte, bsize)

	// get stream
	mr, err := ctx.Request.MultipartReader()
	if err != nil {
		return "", 0, err
	}
	in_f, err := mr.NextPart()
	if err != nil {
		return "", 0, err
	}
	defer in_f.Close()

	// start reading/writing

	for {
		// read
		n, err := in_f.Read(buffer)
		if n == 0 {
			break
		}
		if err != nil {
			return "", 0, err
		}
		// update total bytes
		total += n
		if total > maxbytes {
			return "", 0, fmt.Errorf("exceeded maximum file size of %d bytes", maxbytes)
		}
		// write
		n, err = tmpfile.Write(buffer[:n])
		if err != nil {
			return "", 0, err
		}
	}

	return tmpfile.Name(), total, nil
}

func uploadFileHandler(ctx *gin.Context) {
	ticket := ctx.Params.ByName("ticket")
	filename, _, err := uploadFileHelper(300, ctx)
	if err != nil {
		data := bfs.ErrorResponse(fmt.Errorf("upload failed: %s", err.Error())).JSON()
		ctx.Data(500, "application/json", data)
		return
	}

	cmd := dsl.NewCommand("writebytes", false)
	cmd.Args["ticket"] = ticket
	cmd.Args["tmpfile"] = filename
	c := &core.CommandRequest{cmd, "", make(chan bfs.BFSResponse)}
	CommandsChan <- c
	data := <-c.ResultChannel
	ctx.Data(200, "application/json", data.JSON())
}

func downloadFileHandler(ctx *gin.Context) {
	var form struct {
		Token    string `form:"token" binding:"required"`
		Database string `form:"database" binding:"required"`
		Path     string `form:"path" binding:"required"`
	}
	ok := ctx.Bind(&form)
	if !ok {
		data := bfs.ErrorResponse(fmt.Errorf("Missing parameters")).JSON()
		ctx.Data(400, "application/json", data)
		return
	}

	cmd := dsl.NewCommand("readbytes", false)
	cmd.Database = form.Database
	cmd.Args["path"] = form.Path
	cmd.Args["writer"] = ctx.Writer
	c := &core.CommandRequest{cmd, form.Token, make(chan bfs.BFSResponse)}
	ctx.Writer.Header().Set("Content-Type", "application/octet-stream")
	CommandsChan <- c
	data := <-c.ResultChannel
	if !data.Success() {
		ctx.Data(500, "application/json", data.JSON())
		return
	}
}

func main() {
	app := cli.NewApp()
	createadminCmd := cli.Command{
		Name: "createadmin",
		Flags: []cli.Flag{
			cli.StringFlag{Name: "u", Value: "", Usage: "username"},
			cli.StringFlag{Name: "p", Value: "", Usage: "password"},
			cli.StringFlag{Name: "c", Value: "config.json"},
		},
		Action: func(c *cli.Context) {
			usr := c.String("u")
			pw := c.String("p")
			pth := c.String("c")

			rdr, err := os.Open(pth)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			config, err := simplejson.NewFromReader(rdr)

			err = engine.CreateAdminUser(usr, pw, config.Get("bytengine"))
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			fmt.Println("...done")
		},
	}
	run := cli.Command{
		Name: "run",
		Flags: []cli.Flag{
			cli.StringFlag{Name: "c", Value: "config.json"},
		},
		Action: func(c *cli.Context) {
			// get configuration file/info
			pth := c.String("c")
			rdr, err := os.Open(pth)
			if err != nil {
				fmt.Println("Error: ", err)
				os.Exit(1)
			}
			config, err := simplejson.NewFromReader(rdr)
			wcount := config.Get("workers").MustInt()
			addr := config.Get("address").MustString()
			port := config.Get("port").MustInt()

			// setup channels
			ScriptsChan, CommandsChan = engine.WorkerPool(wcount, config.Get("bytengine"))

			// setup routes
			router := gin.Default()
			router.POST("/bfs/query", runScriptHandler)
			router.POST("/bfs/token", getTokenHandler)
			router.POST("/bfs/uploadticket", getUploadTicketHandler)
			router.POST("/bfs/writebytes/:ticket", uploadFileHandler)
			router.POST("/bfs/readbytes", downloadFileHandler)

			router.Run(fmt.Sprintf("%s:%d", addr, port))
		},
	}
	app.Commands = []cli.Command{createadminCmd, run}
	app.Run(os.Args)
}

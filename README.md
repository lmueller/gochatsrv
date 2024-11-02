# gochatsrv

`gochatsrv` is a chat server application written in Go. It supports multiple user connections, user management, and various chat commands.

## Features

robust concurrency,
simple privilege model
broadcast/single channel (for now)
login/pw through sqlite3,
terminal colors through github.com/lmueller/termcolor
timeable shutdown with warning message ticker
features: /who,/whoami,/kick,/nick,/msg,/reply,/help,/echo,/logout,/shutdown,/createuser,/deleteuser,/enumusers,/priv


## Installation

1. Clone the repository:
    ```sh
    git clone https://github.com/lmueller/gochatsrv.git
    cd gochatsrv
    ```

2. Install dependencies:
    ```sh
    go mod tidy
    ```

3. Build the server:
    ```sh
    go build -o gochatsrv cmd/server/main.go
    ```

## Usage

1. Run the server:
    ```sh
    ./gochatsrv
    ```
   This will create a new database and admin user with the username "admin" and password "admin123".

2. Connect to the server using a TCP client (e.g., `telnet`):
    ```sh
    telnet localhost 8080
    ```

3. Login and change the admin password, then logout.

## Contributing

1. Fork the repository.
2. Create a new branch (`git checkout -b feature-branch`).
3. Make your changes.
4. Commit your changes (`git commit -am 'Add new feature'`).
5. Push to the branch (`git push origin feature-branch`).
6. Create a new Pull Request.

## License

This project is licensed under the proprietary license. All rights reserved.

## Contact

Author: Lutz Mueller  
Email: lmuellerhome@gmail.com
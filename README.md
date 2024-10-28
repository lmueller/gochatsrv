# gochatsrv

`gochatsrv` is a chat server application written in Go. It supports multiple user connections, user management, and various chat commands.

## Features

- Multi-user chat server
- User management (login, logout, nickname change)
- Admin commands (kick, shutdown)
- Private messaging
- Graceful server shutdown

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

2. Connect to the server using a TCP client (e.g., `telnet`):
    ```sh
    telnet localhost 8080
    ```

3. Use the following commands in the chat:
    - `/whoami` - Display your user information
    - `/kick <nickname>` - Kick a user (admin only)
    - `/msg <nickname> <message>` - Send a private message
    - `/nick <newNickname>` - Change your nickname
    - `/shutdown <seconds>` - Shutdown the server after a delay (admin only)

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
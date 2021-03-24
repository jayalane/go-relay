import socket


bytesToSend = b"Hi there how is it going?"

UDPClientSocket = socket.socket(family=socket.AF_INET, type=socket.SOCK_DGRAM)

 

# Send to server using created UDP socket

UDPClientSocket.sendto(bytesToSend, ('127.0.0.1', 1053))
 

msgFromServer = UDPClientSocket.recvfrom(65536)

 

msg = "Message from Server {}".format(msgFromServer[0])

print(msg)

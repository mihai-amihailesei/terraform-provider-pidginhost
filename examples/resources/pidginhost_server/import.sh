# Import a server by its ID. Create-only attributes that the API does not
# return (image, user_data, password, ...) can then be added to the
# configuration without forcing a replacement.
terraform import pidginhost_server.web 123
